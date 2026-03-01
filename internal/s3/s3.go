// Package s3client wraps AWS S3 operations for helium-sync.
package s3client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/helium-sync/helium-sync/internal/config"
)

const (
	multipartThreshold = 8 * 1024 * 1024 // 8MB
	profilePrefix      = "helium-profiles/"
)

// Manifest represents the remote sync manifest for a profile.
type Manifest struct {
	DeviceID          string            `json:"device_id"`
	LastSyncTimestamp time.Time         `json:"last_sync_timestamp"`
	FileChecksums     map[string]string `json:"file_checksums"`
}

// Client wraps the S3 client.
type Client struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	sse      bool
}

// New creates a new S3 client from the helium-sync config.
func New(cfg *config.Config) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.S3Region))
	if cfg.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.AWSProfile))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg)
	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		u.PartSize = multipartThreshold
	})

	return &Client{
		client:   s3Client,
		uploader: uploader,
		bucket:   cfg.S3Bucket,
		sse:      cfg.SSE,
	}, nil
}

// profileKey returns the S3 key prefix for a profile.
func profileKey(profileName string) string {
	return profilePrefix + profileName + "/"
}

// ManifestKey returns the S3 key for a profile's manifest.
func ManifestKey(profileName string) string {
	return profileKey(profileName) + "manifest.json"
}

// FileKey returns the S3 key for a specific file within a profile.
func FileKey(profileName, relPath string) string {
	return profileKey(profileName) + "files/" + relPath
}

// GetManifest downloads and parses the manifest for a profile.
func (c *Client) GetManifest(ctx context.Context, profileName string) (*Manifest, error) {
	key := ManifestKey(profileName)
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if not found
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting manifest: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("reading manifest body: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &manifest, nil
}

// PutManifest uploads the manifest for a profile.
func (c *Client) PutManifest(ctx context.Context, profileName string, manifest *Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(ManifestKey(profileName)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	}
	if c.sse {
		input.ServerSideEncryption = types.ServerSideEncryptionAes256
	}

	_, err = c.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("putting manifest: %w", err)
	}
	return nil
}

// UploadFile uploads a file to S3 with multipart for files > 8MB.
func (c *Client) UploadFile(ctx context.Context, profileName, relPath, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", localPath, err)
	}
	defer f.Close()

	key := FileKey(profileName, relPath)
	input := &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   f,
	}
	if c.sse {
		input.ServerSideEncryption = types.ServerSideEncryptionAes256
	}

	// Use uploader which handles multipart automatically
	_, err = c.uploader.Upload(ctx, input)
	if err != nil {
		return fmt.Errorf("uploading %s: %w", relPath, err)
	}
	return nil
}

// DownloadFile downloads a file from S3 to a local path.
func (c *Client) DownloadFile(ctx context.Context, profileName, relPath, localPath string) error {
	key := FileKey(profileName, relPath)
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("getting %s: %w", relPath, err)
	}
	defer result.Body.Close()

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", localPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, result.Body); err != nil {
		return fmt.Errorf("writing file %s: %w", localPath, err)
	}
	return nil
}

// DeleteFile deletes a file from S3.
func (c *Client) DeleteFile(ctx context.Context, profileName, relPath string) error {
	key := FileKey(profileName, relPath)
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("deleting %s: %w", relPath, err)
	}
	return nil
}

// ListBuckets returns available S3 buckets.
func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	result, err := c.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("listing buckets: %w", err)
	}
	var names []string
	for _, b := range result.Buckets {
		names = append(names, aws.ToString(b.Name))
	}
	return names, nil
}

// CreateBucket creates a new S3 bucket.
func (c *Client) CreateBucket(ctx context.Context, name, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	if region != "" && region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	_, err := c.client.CreateBucket(ctx, input)
	if err != nil {
		return fmt.Errorf("creating bucket: %w", err)
	}
	return nil
}

// ListRemoteProfiles lists profile names in the bucket.
func (c *Client) ListRemoteProfiles(ctx context.Context) ([]string, error) {
	result, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(c.bucket),
		Prefix:    aws.String(profilePrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("listing profiles: %w", err)
	}

	var profiles []string
	for _, prefix := range result.CommonPrefixes {
		name := aws.ToString(prefix.Prefix)
		name = strings.TrimPrefix(name, profilePrefix)
		name = strings.TrimSuffix(name, "/")
		if name != "" {
			profiles = append(profiles, name)
		}
	}
	return profiles, nil
}

// ValidateAccess checks if We can access the bucket.
func (c *Client) ValidateAccess(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return fmt.Errorf("cannot access bucket %s: %w", c.bucket, err)
	}
	return nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "NoSuchKey") ||
		strings.Contains(errStr, "NotFound") ||
		strings.Contains(errStr, "404")
}

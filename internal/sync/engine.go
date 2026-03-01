// Package sync implements the core synchronization engine for helium-sync.
package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/helium-sync/helium-sync/internal/config"
	"github.com/helium-sync/helium-sync/internal/profile"
	s3client "github.com/helium-sync/helium-sync/internal/s3"
)

// Delta describes what changed for a file.
type Delta struct {
	RelPath      string
	LocalOnly    bool
	RemoteOnly   bool
	LocalChanged bool
	RemoteChanged bool
	BothChanged  bool
}

// SyncResult contains the result of a sync operation.
type SyncResult struct {
	ProfileName string
	Uploaded    int
	Downloaded  int
	Deleted     int
	Skipped     int
	Errors      []string
}

// Engine performs sync operations.
type Engine struct {
	cfg    *config.Config
	s3     *s3client.Client
	logger *Logger
}

// NewEngine creates a new sync engine.
func NewEngine(cfg *config.Config) (*Engine, error) {
	client, err := s3client.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}
	return &Engine{
		cfg:    cfg,
		s3:     client,
		logger: WithComponent("sync"),
	}, nil
}

// AcquireLock attempts to acquire a file lock.
func AcquireLock() (*os.File, error) {
	lockPath := config.LockFilePath()
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating lock dir: %w", err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("another sync is already running")
	}

	return f, nil
}

// ReleaseLock releases the file lock.
func ReleaseLock(f *os.File) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(config.LockFilePath())
	}
}

// SyncProfile synchronizes a single profile.
func (e *Engine) SyncProfile(ctx context.Context, prof profile.Profile, interactive bool) (*SyncResult, error) {
	e.logger.Infof("starting sync for profile: %s", prof.Name)
	result := &SyncResult{ProfileName: prof.Name}

	// Check if Helium holds the profile lock
	if profile.IsLocked(prof.Path) {
		e.logger.Warn("profile is locked by Helium, skipping sync")
		return nil, fmt.Errorf("profile %s is locked by Helium browser", prof.Name)
	}

	// Step 1-2: Scan local profile and build checksum map
	e.logger.Debug("scanning local profile")
	localFiles, err := profile.ScanProfile(prof.Path)
	if err != nil {
		return nil, fmt.Errorf("scanning profile %s: %w", prof.Name, err)
	}

	localChecksums := make(map[string]string)
	localFileMap := make(map[string]profile.FileEntry)
	for _, f := range localFiles {
		localChecksums[f.RelPath] = f.Checksum
		localFileMap[f.RelPath] = f
	}

	// Step 3: Download remote manifest
	e.logger.Debug("downloading remote manifest")
	deviceID, err := config.GetDeviceID()
	if err != nil {
		return nil, fmt.Errorf("getting device ID: %w", err)
	}

	remoteManifest, err := e.s3.GetManifest(ctx, prof.Name)
	if err != nil {
		return nil, fmt.Errorf("getting remote manifest: %w", err)
	}

	// No remote manifest - initial upload
	if remoteManifest == nil {
		e.logger.Info("no remote manifest found, performing initial upload")
		return e.initialUpload(ctx, prof, localFiles, localChecksums, deviceID)
	}

	// Step 4: Compute delta
	e.logger.Debug("computing delta")
	deltas := computeDeltas(localChecksums, remoteManifest.FileChecksums)

	// Check for conflicts
	hasConflicts := false
	for _, d := range deltas {
		if d.BothChanged {
			hasConflicts = true
			break
		}
	}

	if hasConflicts && interactive {
		// In interactive mode, conflicts need to be resolved by the caller
		e.logger.Warn("conflicts detected during sync")
		return nil, fmt.Errorf("CONFLICT: both local and remote have changes since last sync")
	}

	// Step 5: Upload/download changed files
	for _, d := range deltas {
		if d.LocalOnly || d.LocalChanged || d.BothChanged {
			// Upload local file
			entry, ok := localFileMap[d.RelPath]
			if !ok {
				continue
			}
			e.logger.Debugf("uploading: %s", d.RelPath)
			if err := e.s3.UploadFile(ctx, prof.Name, d.RelPath, entry.FullPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", d.RelPath, err))
				continue
			}
			result.Uploaded++
		} else if d.RemoteOnly || d.RemoteChanged {
			// Download from remote
			localPath := filepath.Join(prof.Path, d.RelPath)
			e.logger.Debugf("downloading: %s", d.RelPath)
			if err := e.s3.DownloadFile(ctx, prof.Name, d.RelPath, localPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", d.RelPath, err))
				continue
			}
			result.Downloaded++
		}
	}

	// Handle files deleted locally (present in remote but not locally)
	for relPath := range remoteManifest.FileChecksums {
		if _, exists := localChecksums[relPath]; !exists {
			// File was deleted locally - also remove from remote
			e.logger.Debugf("removing from remote (deleted locally): %s", relPath)
			if err := e.s3.DeleteFile(ctx, prof.Name, relPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("delete %s: %v", relPath, err))
				continue
			}
			result.Deleted++
		}
	}

	// Step 6: Update remote manifest
	now := time.Now().UTC()
	newManifest := &s3client.Manifest{
		DeviceID:          deviceID,
		LastSyncTimestamp:  now,
		FileChecksums:     localChecksums,
	}
	if err := e.s3.PutManifest(ctx, prof.Name, newManifest); err != nil {
		return nil, fmt.Errorf("updating manifest: %w", err)
	}

	// Step 7: Update local last sync time
	e.saveLastSyncTime(prof.Name, now)

	e.logger.Infof("sync complete for %s: uploaded=%d downloaded=%d deleted=%d errors=%d",
		prof.Name, result.Uploaded, result.Downloaded, result.Deleted, len(result.Errors))

	return result, nil
}

// SyncAllProfiles syncs all discovered profiles.
func (e *Engine) SyncAllProfiles(ctx context.Context, interactive bool) ([]*SyncResult, error) {
	profiles, err := profile.Discover(e.cfg.HeliumDir)
	if err != nil {
		return nil, fmt.Errorf("discovering profiles: %w", err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no profiles found in %s", e.cfg.HeliumDir)
	}

	var results []*SyncResult
	for _, prof := range profiles {
		result, err := e.SyncProfile(ctx, prof, interactive)
		if err != nil {
			e.logger.Error(fmt.Sprintf("sync failed for %s", prof.Name), err)
			results = append(results, &SyncResult{
				ProfileName: prof.Name,
				Errors:      []string{err.Error()},
			})
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

// RestoreProfile downloads all files for a profile from S3.
func (e *Engine) RestoreProfile(ctx context.Context, prof profile.Profile) (*SyncResult, error) {
	e.logger.Infof("restoring profile: %s", prof.Name)
	result := &SyncResult{ProfileName: prof.Name}

	manifest, err := e.s3.GetManifest(ctx, prof.Name)
	if err != nil {
		return nil, fmt.Errorf("getting manifest: %w", err)
	}
	if manifest == nil {
		return nil, fmt.Errorf("no remote data found for profile %s", prof.Name)
	}

	for relPath := range manifest.FileChecksums {
		if !profile.IsAllowed(relPath) {
			result.Skipped++
			continue
		}
		localPath := filepath.Join(prof.Path, relPath)
		e.logger.Debugf("restoring: %s", relPath)
		if err := e.s3.DownloadFile(ctx, prof.Name, relPath, localPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("restore %s: %v", relPath, err))
			continue
		}
		result.Downloaded++
	}

	deviceID, _ := config.GetDeviceID()
	now := time.Now().UTC()
	newManifest := &s3client.Manifest{
		DeviceID:          deviceID,
		LastSyncTimestamp:  now,
		FileChecksums:     manifest.FileChecksums,
	}
	e.s3.PutManifest(ctx, prof.Name, newManifest)
	e.saveLastSyncTime(prof.Name, now)

	e.logger.Infof("restore complete for %s: downloaded=%d errors=%d",
		prof.Name, result.Downloaded, len(result.Errors))
	return result, nil
}

// GetRemoteProfiles returns a list of profiles in S3.
func (e *Engine) GetRemoteProfiles(ctx context.Context) ([]string, error) {
	return e.s3.ListRemoteProfiles(ctx)
}

func (e *Engine) initialUpload(ctx context.Context, prof profile.Profile, files []profile.FileEntry, checksums map[string]string, deviceID string) (*SyncResult, error) {
	result := &SyncResult{ProfileName: prof.Name}

	for _, f := range files {
		e.logger.Debugf("uploading (initial): %s", f.RelPath)
		if err := e.s3.UploadFile(ctx, prof.Name, f.RelPath, f.FullPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", f.RelPath, err))
			continue
		}
		result.Uploaded++
	}

	now := time.Now().UTC()
	manifest := &s3client.Manifest{
		DeviceID:          deviceID,
		LastSyncTimestamp:  now,
		FileChecksums:     checksums,
	}
	if err := e.s3.PutManifest(ctx, prof.Name, manifest); err != nil {
		return nil, fmt.Errorf("initial manifest upload: %w", err)
	}

	e.saveLastSyncTime(prof.Name, now)
	return result, nil
}

func computeDeltas(localChecksums, remoteChecksums map[string]string) []Delta {
	var deltas []Delta

	allPaths := make(map[string]bool)
	for p := range localChecksums {
		allPaths[p] = true
	}
	for p := range remoteChecksums {
		allPaths[p] = true
	}

	for path := range allPaths {
		localSum, hasLocal := localChecksums[path]
		remoteSum, hasRemote := remoteChecksums[path]

		if hasLocal && !hasRemote {
			deltas = append(deltas, Delta{RelPath: path, LocalOnly: true})
		} else if !hasLocal && hasRemote {
			deltas = append(deltas, Delta{RelPath: path, RemoteOnly: true})
		} else if localSum != remoteSum {
			// Both exist but different - last writer wins in background
			deltas = append(deltas, Delta{RelPath: path, LocalChanged: true})
		}
		// If checksums match, no delta
	}

	return deltas
}

func (e *Engine) saveLastSyncTime(profileName string, t time.Time) {
	stateDir := config.StateDir()
	os.MkdirAll(stateDir, 0700)
	path := filepath.Join(stateDir, fmt.Sprintf("last_sync_%s", profileName))
	os.WriteFile(path, []byte(t.Format(time.RFC3339)), 0600)
}

// GetLastSyncTime reads the last sync time for a profile.
func GetLastSyncTime(profileName string) (time.Time, error) {
	path := filepath.Join(config.StateDir(), fmt.Sprintf("last_sync_%s", profileName))
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, string(data))
}

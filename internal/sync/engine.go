// Package sync implements the core synchronization engine for helium-sync.
package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stonespren/helium-sync/internal/config"
	"github.com/stonespren/helium-sync/internal/profile"
	s3client "github.com/stonespren/helium-sync/internal/s3"
)

const (
	// Minimum seconds between sync runs to debounce rapid changes.
	debounceSeconds = 30
)

// Delta describes what changed for a file using three-way comparison.
type Delta struct {
	RelPath       string
	LocalOnly     bool // exists locally but not in baseline or remote (new local)
	RemoteOnly    bool // exists in remote but not in baseline or local (new remote)
	LocalChanged  bool // local differs from baseline, remote matches baseline
	RemoteChanged bool // remote differs from baseline, local matches baseline
	BothChanged   bool // both local and remote differ from baseline
	LocalDeleted  bool // was in baseline, gone locally, still in remote
	RemoteDeleted bool // was in baseline, gone remotely, still locally
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

// ConflictChoice represents a user's conflict resolution choice.
type ConflictChoice int

const (
	ChoiceKeepLocal  ConflictChoice = 1
	ChoiceKeepRemote ConflictChoice = 2
)

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

// AcquireLock attempts to acquire a file lock for safe concurrent execution.
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

// SyncProfile synchronizes a single profile using three-way merge.
func (e *Engine) SyncProfile(ctx context.Context, prof profile.Profile, interactive bool) (*SyncResult, error) {
	e.logger.Infof("starting sync for profile: %s", prof.Name)
	result := &SyncResult{ProfileName: prof.Name}

	// Debounce: skip if last sync was too recent
	if lastSync, err := GetLastSyncTime(prof.Name); err == nil {
		elapsed := time.Since(lastSync).Seconds()
		if elapsed < debounceSeconds {
			e.logger.Infof("debounce: last sync was %.0fs ago, skipping (threshold %ds)", elapsed, debounceSeconds)
			return result, nil
		}
	}

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

	// No remote manifest — first upload
	if remoteManifest == nil {
		e.logger.Info("no remote manifest found, performing initial upload")
		res, err := e.initialUpload(ctx, prof, localFiles, localChecksums, deviceID)
		if err != nil {
			return nil, err
		}
		e.saveLocalManifest(prof.Name, localChecksums)
		return res, nil
	}

	// Load the baseline (local copy of manifest from last successful sync)
	baseline := e.loadLocalManifest(prof.Name)

	// Step 4: Compute three-way delta
	e.logger.Debug("computing three-way delta")
	deltas := computeThreeWayDeltas(baseline, localChecksums, remoteManifest.FileChecksums)

	if len(deltas) == 0 {
		e.logger.Info("no changes detected")
		e.saveLastSyncTime(prof.Name, time.Now().UTC())
		return result, nil
	}

	// Check for conflicts (BothChanged)
	var conflicts []Delta
	for _, d := range deltas {
		if d.BothChanged {
			conflicts = append(conflicts, d)
		}
	}

	// Resolve conflicts
	conflictChoice := ChoiceKeepLocal // default: last-writer-wins in background
	if len(conflicts) > 0 {
		if interactive {
			choice, err := promptConflictResolution(conflicts)
			if err != nil {
				return nil, fmt.Errorf("conflict resolution: %w", err)
			}
			conflictChoice = choice
		} else {
			// Background: newer last_sync_timestamp wins (last-writer-wins)
			localLastSync, _ := GetLastSyncTime(prof.Name)
			if remoteManifest.LastSyncTimestamp.After(localLastSync) {
				conflictChoice = ChoiceKeepRemote
			} else {
				conflictChoice = ChoiceKeepLocal
			}
			e.logger.Infof("background conflict resolution: keeping %s (last-writer-wins)",
				map[ConflictChoice]string{ChoiceKeepLocal: "local", ChoiceKeepRemote: "remote"}[conflictChoice])
		}
	}

	// Step 5: Apply deltas
	// Build the merged checksum map for the new manifest
	mergedChecksums := make(map[string]string)
	// Start with everything that's unchanged
	for k, v := range localChecksums {
		mergedChecksums[k] = v
	}

	for _, d := range deltas {
		switch {
		case d.LocalOnly:
			// New local file — upload
			entry, ok := localFileMap[d.RelPath]
			if !ok {
				continue
			}
			e.logger.Debugf("uploading (new local): %s", d.RelPath)
			if err := e.s3.UploadFile(ctx, prof.Name, d.RelPath, entry.FullPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", d.RelPath, err))
				continue
			}
			result.Uploaded++
			mergedChecksums[d.RelPath] = localChecksums[d.RelPath]

		case d.RemoteOnly:
			// New remote file — download
			localPath := filepath.Join(prof.Path, d.RelPath)
			e.logger.Debugf("downloading (new remote): %s", d.RelPath)
			if err := e.s3.DownloadFile(ctx, prof.Name, d.RelPath, localPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", d.RelPath, err))
				continue
			}
			result.Downloaded++
			mergedChecksums[d.RelPath] = remoteManifest.FileChecksums[d.RelPath]

		case d.LocalChanged:
			// Local changed, remote same as baseline — upload
			entry, ok := localFileMap[d.RelPath]
			if !ok {
				continue
			}
			e.logger.Debugf("uploading (local changed): %s", d.RelPath)
			if err := e.s3.UploadFile(ctx, prof.Name, d.RelPath, entry.FullPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", d.RelPath, err))
				continue
			}
			result.Uploaded++
			mergedChecksums[d.RelPath] = localChecksums[d.RelPath]

		case d.RemoteChanged:
			// Remote changed, local same as baseline — download
			localPath := filepath.Join(prof.Path, d.RelPath)
			e.logger.Debugf("downloading (remote changed): %s", d.RelPath)
			if err := e.s3.DownloadFile(ctx, prof.Name, d.RelPath, localPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", d.RelPath, err))
				continue
			}
			result.Downloaded++
			mergedChecksums[d.RelPath] = remoteManifest.FileChecksums[d.RelPath]

		case d.BothChanged:
			// Conflict — use resolution choice
			if conflictChoice == ChoiceKeepLocal {
				entry, ok := localFileMap[d.RelPath]
				if !ok {
					continue
				}
				e.logger.Debugf("uploading (conflict, keep local): %s", d.RelPath)
				if err := e.s3.UploadFile(ctx, prof.Name, d.RelPath, entry.FullPath); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("upload %s: %v", d.RelPath, err))
					continue
				}
				result.Uploaded++
				mergedChecksums[d.RelPath] = localChecksums[d.RelPath]
			} else {
				localPath := filepath.Join(prof.Path, d.RelPath)
				e.logger.Debugf("downloading (conflict, keep remote): %s", d.RelPath)
				if err := e.s3.DownloadFile(ctx, prof.Name, d.RelPath, localPath); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("download %s: %v", d.RelPath, err))
					continue
				}
				result.Downloaded++
				mergedChecksums[d.RelPath] = remoteManifest.FileChecksums[d.RelPath]
			}

		case d.LocalDeleted:
			// File deleted locally, still in remote — remove from remote
			e.logger.Debugf("deleting from remote (deleted locally): %s", d.RelPath)
			if err := e.s3.DeleteFile(ctx, prof.Name, d.RelPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("delete %s: %v", d.RelPath, err))
				continue
			}
			result.Deleted++
			delete(mergedChecksums, d.RelPath)

		case d.RemoteDeleted:
			// File deleted remotely, still locally — remove local
			localPath := filepath.Join(prof.Path, d.RelPath)
			e.logger.Debugf("deleting local (deleted remotely): %s", d.RelPath)
			if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
				result.Errors = append(result.Errors, fmt.Sprintf("local delete %s: %v", d.RelPath, err))
				continue
			}
			result.Deleted++
			delete(mergedChecksums, d.RelPath)
		}
	}

	// Step 6: Update remote manifest
	now := time.Now().UTC()
	newManifest := &s3client.Manifest{
		DeviceID:         deviceID,
		LastSyncTimestamp: now,
		FileChecksums:    mergedChecksums,
	}
	if err := e.s3.PutManifest(ctx, prof.Name, newManifest); err != nil {
		return nil, fmt.Errorf("updating manifest: %w", err)
	}

	// Step 7: Update local state
	e.saveLastSyncTime(prof.Name, now)
	e.saveLocalManifest(prof.Name, mergedChecksums)

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
		DeviceID:         deviceID,
		LastSyncTimestamp: now,
		FileChecksums:    manifest.FileChecksums,
	}
	if err := e.s3.PutManifest(ctx, prof.Name, newManifest); err != nil {
		e.logger.Error("failed to update manifest after restore", err)
	}
	e.saveLastSyncTime(prof.Name, now)
	e.saveLocalManifest(prof.Name, manifest.FileChecksums)

	e.logger.Infof("restore complete for %s: downloaded=%d errors=%d",
		prof.Name, result.Downloaded, len(result.Errors))
	return result, nil
}

// GetRemoteProfiles returns a list of profiles in S3.
func (e *Engine) GetRemoteProfiles(ctx context.Context) ([]string, error) {
	return e.s3.ListRemoteProfiles(ctx)
}

// initialUpload uploads all allowed files for the first time.
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
		DeviceID:         deviceID,
		LastSyncTimestamp: now,
		FileChecksums:    checksums,
	}
	if err := e.s3.PutManifest(ctx, prof.Name, manifest); err != nil {
		return nil, fmt.Errorf("initial manifest upload: %w", err)
	}

	e.saveLastSyncTime(prof.Name, now)
	return result, nil
}

// computeThreeWayDeltas computes deltas using baseline, local, and remote checksums.
// baseline is the state at last successful sync (nil map for first sync on this device).
// This enables correct bidirectional sync: we can tell whether a difference is
// a local change, a remote change, or a conflict.
func computeThreeWayDeltas(baseline, local, remote map[string]string) []Delta {
	var deltas []Delta

	allPaths := make(map[string]bool)
	for p := range baseline {
		allPaths[p] = true
	}
	for p := range local {
		allPaths[p] = true
	}
	for p := range remote {
		allPaths[p] = true
	}

	for path := range allPaths {
		baseSum, inBase := baseline[path]
		localSum, inLocal := local[path]
		remoteSum, inRemote := remote[path]

		localSame := inLocal && inBase && localSum == baseSum
		remoteSame := inRemote && inBase && remoteSum == baseSum
		localNew := inLocal && !inBase
		remoteNew := inRemote && !inBase
		localGone := !inLocal && inBase
		remoteGone := !inRemote && inBase

		switch {
		// Both match baseline or each other — no change
		case inLocal && inRemote && localSum == remoteSum:
			continue

		// New file only on local side (not in baseline, not in remote)
		case localNew && !inRemote:
			deltas = append(deltas, Delta{RelPath: path, LocalOnly: true})

		// New file only on remote side (not in baseline, not in local)
		case remoteNew && !inLocal:
			deltas = append(deltas, Delta{RelPath: path, RemoteOnly: true})

		// Local changed from baseline, remote unchanged
		case inLocal && inRemote && !localSame && remoteSame:
			deltas = append(deltas, Delta{RelPath: path, LocalChanged: true})

		// Remote changed from baseline, local unchanged
		case inLocal && inRemote && localSame && !remoteSame:
			deltas = append(deltas, Delta{RelPath: path, RemoteChanged: true})

		// Both changed from baseline differently
		case inLocal && inRemote && !localSame && !remoteSame && localSum != remoteSum:
			deltas = append(deltas, Delta{RelPath: path, BothChanged: true})

		// File deleted locally (was in baseline, still in remote, gone locally)
		case localGone && inRemote && remoteSame:
			deltas = append(deltas, Delta{RelPath: path, LocalDeleted: true})

		// File deleted remotely (was in baseline, still locally, gone in remote)
		case remoteGone && inLocal && localSame:
			deltas = append(deltas, Delta{RelPath: path, RemoteDeleted: true})

		// File deleted locally but remote also changed — conflict, treat as BothChanged
		case localGone && inRemote && !remoteSame:
			deltas = append(deltas, Delta{RelPath: path, BothChanged: true})

		// File deleted remotely but local also changed — conflict, treat as BothChanged
		case remoteGone && inLocal && !localSame:
			deltas = append(deltas, Delta{RelPath: path, BothChanged: true})

		// Both new but different content
		case localNew && remoteNew && localSum != remoteSum:
			deltas = append(deltas, Delta{RelPath: path, BothChanged: true})

		// Both deleted — nothing to do
		case localGone && remoteGone:
			continue

		// Fallback: if local and remote differ, treat as local changed (last-writer-wins)
		default:
			if inLocal && (!inRemote || localSum != remoteSum) {
				deltas = append(deltas, Delta{RelPath: path, LocalChanged: true})
			} else if inRemote && !inLocal {
				deltas = append(deltas, Delta{RelPath: path, RemoteOnly: true})
			}
		}
	}

	return deltas
}

// promptConflictResolution shows conflicting files and asks the user to choose.
func promptConflictResolution(conflicts []Delta) (ConflictChoice, error) {
	fmt.Println("\nConflict detected: both local and remote have changed since last sync.")
	fmt.Println("The following files are in conflict:")
	for _, c := range conflicts {
		fmt.Printf("  - %s\n", c.RelPath)
	}
	fmt.Println("\nChoose resolution:")
	fmt.Println("  1) Keep local (upload local versions)")
	fmt.Println("  2) Keep remote (download remote versions)")
	fmt.Print("Choice [1/2]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	switch input {
	case "1":
		return ChoiceKeepLocal, nil
	case "2":
		return ChoiceKeepRemote, nil
	default:
		return ChoiceKeepLocal, fmt.Errorf("invalid choice: %q, aborting", input)
	}
}

// --- Local manifest persistence ---

// localManifestPath returns the path where we store the baseline checksums.
func localManifestPath(profileName string) string {
	return filepath.Join(config.StateDir(), fmt.Sprintf("manifest_%s.json", profileName))
}

// saveLocalManifest persists the merged checksum state after a successful sync.
func (e *Engine) saveLocalManifest(profileName string, checksums map[string]string) {
	stateDir := config.StateDir()
	os.MkdirAll(stateDir, 0700)
	data, err := json.Marshal(checksums)
	if err != nil {
		e.logger.Error("failed to marshal local manifest", err)
		return
	}
	if err := os.WriteFile(localManifestPath(profileName), data, 0600); err != nil {
		e.logger.Error("failed to save local manifest", err)
	}
}

// loadLocalManifest reads the baseline checksum state from last successful sync.
func (e *Engine) loadLocalManifest(profileName string) map[string]string {
	data, err := os.ReadFile(localManifestPath(profileName))
	if err != nil {
		return make(map[string]string) // empty baseline = first sync on this device
	}
	var checksums map[string]string
	if err := json.Unmarshal(data, &checksums); err != nil {
		e.logger.Warn("corrupt local manifest, treating as first sync")
		return make(map[string]string)
	}
	return checksums
}

// --- Time persistence ---

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

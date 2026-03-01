package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var AllowedPaths = []string{
	"Preferences",
	"Bookmarks",
	"History",
	"Extensions",
	"Local Extension Settings",
}

var ExcludedNames = []string{
	"Cookies",
	"Login Data",
	"Web Data",
	"Network",
	"Cache",
	"GPUCache",
	"Code Cache",
	"Service Worker",
	"Sessions",
}

type Profile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type LocalState struct {
	Profile struct {
		InfoCache map[string]json.RawMessage `json:"info_cache"`
	} `json:"profile"`
}

func Discover(heliumDir string) ([]Profile, error) {
	localStatePath := filepath.Join(heliumDir, "Local State")
	data, err := os.ReadFile(localStatePath)
	if err != nil {
		defaultPath := filepath.Join(heliumDir, "Default")
		if info, serr := os.Stat(defaultPath); serr == nil && info.IsDir() {
			return []Profile{{Name: "Default", Path: defaultPath}}, nil
		}
		return nil, fmt.Errorf("reading Local State: %w", err)
	}

	var state LocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing Local State: %w", err)
	}

	var profiles []Profile
	for name := range state.Profile.InfoCache {
		profilePath := filepath.Join(heliumDir, name)
		if info, err := os.Stat(profilePath); err == nil && info.IsDir() {
			profiles = append(profiles, Profile{
				Name: name,
				Path: profilePath,
			})
		}
	}

	if len(profiles) == 0 {
		for _, name := range []string{"Default", "Profile 1", "Profile 2", "Profile 3"} {
			profilePath := filepath.Join(heliumDir, name)
			if info, err := os.Stat(profilePath); err == nil && info.IsDir() {
				profiles = append(profiles, Profile{
					Name: name,
					Path: profilePath,
				})
			}
		}
	}

	return profiles, nil
}

func IsAllowed(relPath string) bool {
	if strings.Contains(relPath, "..") {
		return false
	}

	parts := strings.Split(relPath, string(os.PathSeparator))
	for _, part := range parts {
		for _, excl := range ExcludedNames {
			if part == excl {
				return false
			}
		}
		if strings.HasSuffix(strings.ToLower(part), ".ldb") {
			return false
		}
	}

	for _, allowed := range AllowedPaths {
		if relPath == allowed || strings.HasPrefix(relPath, allowed+string(os.PathSeparator)) {
			return true
		}
	}

	return false
}

type FileEntry struct {
	RelPath  string
	FullPath string
	Checksum string
	Size     int64
}

func ScanProfile(profilePath string) ([]FileEntry, error) {
	var entries []FileEntry

	for _, allowed := range AllowedPaths {
		fullPath := filepath.Join(profilePath, allowed)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			err = filepath.Walk(fullPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if fi.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(profilePath, path)
				if err != nil {
					return nil
				}
				if !IsAllowed(rel) {
					return nil
				}
				checksum, err := checksumFile(path)
				if err != nil {
					return nil
				}
				entries = append(entries, FileEntry{
					RelPath:  rel,
					FullPath: path,
					Checksum: checksum,
					Size:     fi.Size(),
				})
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walking %s: %w", allowed, err)
			}
		} else {
			rel, _ := filepath.Rel(profilePath, fullPath)
			if !IsAllowed(rel) {
				continue
			}
			checksum, err := checksumFile(fullPath)
			if err != nil {
				continue
			}
			entries = append(entries, FileEntry{
				RelPath:  rel,
				FullPath: fullPath,
				Checksum: checksum,
				Size:     info.Size(),
			})
		}
	}

	return entries, nil
}

func checksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func IsLocked(profilePath string) bool {
	lockFiles := []string{"lockfile", "Lock", "SingletonLock"}
	for _, lf := range lockFiles {
		lockPath := filepath.Join(profilePath, lf)
		if _, err := os.Stat(lockPath); err == nil {
			return true
		}
	}
	parentDir := filepath.Dir(profilePath)
	singletonLock := filepath.Join(parentDir, "SingletonLock")
	if _, err := os.Stat(singletonLock); err == nil {
		return true
	}
	return false
}

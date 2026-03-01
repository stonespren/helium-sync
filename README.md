# helium-sync

Synchronize selected Helium browser profile data across devices using Amazon S3.

## What it does

helium-sync copies a strict subset of Helium browser profile data to and from an S3 bucket. It runs as a systemd user timer, performing incremental syncs at a configurable interval.

**Synced data:** Preferences, Bookmarks, History, Extensions/, Local Extension Settings/

**Never synced:** Cookies, Login Data, Web Data, Network/, Cache/, GPUCache/, Code Cache/, Service Worker/, Sessions/, \*.ldb files

## Installation

### Arch Linux (AUR)

```
git clone https://github.com/stonespren/helium-sync.git
cd helium-sync/packaging/arch
makepkg -si
```

### From source

Requirements: Go 1.21+, AWS CLI v2

```
git clone https://github.com/stonespren/helium-sync.git
cd helium-sync
CGO_ENABLED=0 go build -o helium-sync ./cmd/helium-sync/
sudo install -Dm755 helium-sync /usr/bin/helium-sync
```

## Setup

Run the setup script:

```
./scripts/setup.sh
```

The script will:

1. Install required dependencies (if on Arch Linux)
2. Configure AWS credentials (uses existing `~/.aws/credentials` or creates new)
3. Select or create an S3 bucket
4. Set the Helium browser config path (default: `~/.config/net.imput.helium/`)
5. Detect existing profiles
6. Set the sync interval
7. Create and enable the systemd timer
8. Optionally run an initial sync

### Non-interactive setup

```
./scripts/setup.sh --non-interactive --config /path/to/config.json
```

### Uninstall

```
./scripts/setup.sh --uninstall
```

## CLI usage

### Sync

```
# Sync all profiles
helium-sync sync

# Sync a specific profile
helium-sync sync --profile Default
```

### Restore

```
# Interactive profile selection
helium-sync restore

# Restore all profiles
helium-sync restore --all
```

Restore always prompts before overwriting local data.

### Status

```
helium-sync status
```

Displays: last sync time, next scheduled sync, sync enabled/disabled, tracked profiles, health status.

### Configuration

```
# Show config
helium-sync config

# Edit config
helium-sync config --edit
helium-sync config -e
```

### Logs

```
helium-sync logs
```

### Enable/disable automatic sync

```
helium-sync --enable
helium-sync --disable
```

### Help

```
helium-sync help
```

## Configuration

Config file: `~/.config/helium-sync/config.json`

```json
{
  "helium_dir": "/home/user/.config/net.imput.helium",
  "s3_bucket": "my-helium-sync-bucket",
  "s3_region": "us-east-1",
  "aws_profile": "default",
  "sync_interval_minutes": 15,
  "log_level": "info",
  "sse_s3": true
}
```

Logs: `~/.local/state/helium-sync/helium-sync.log`

Device ID: `~/.config/helium-sync/device_id`

## How sync works

1. Scan local profile for allowed files
2. Compute SHA256 checksums for each file
3. Download the remote manifest from S3
4. Compare checksums to determine what changed
5. Upload new/changed local files, download new/changed remote files
6. Update the remote manifest
7. Record the last sync timestamp locally

Only changed files are transferred. Files larger than 8MB use multipart upload.

## S3 layout

```
s3://<bucket>/helium-profiles/<profile_name>/
├── manifest.json
└── files/
    ├── Preferences
    ├── Bookmarks
    ├── History
    ├── Extensions/...
    └── Local Extension Settings/...
```

## Conflict resolution

- Background sync uses last-writer-wins (based on `last_sync_timestamp`)
- Manual sync shows a summary of differences when both sides changed
- The `restore` command always prompts before overwriting

## Troubleshooting

### Sync fails with "profile is locked"

Helium browser is running and holds a lock on the profile directory. Close Helium or wait for the next sync cycle.

### "another sync is already running"

A previous sync process is still running or crashed without releasing its lock. If no sync is running, remove the lock file:

```
rm ~/.config/helium-sync/helium-sync.lock
```

### AWS credential errors

Verify your credentials:

```
aws sts get-caller-identity --profile <your-profile>
```

Re-run setup to reconfigure:

```
./scripts/setup.sh
```

### Timer not running

Check timer status:

```
systemctl --user status helium-sync.timer
systemctl --user status helium-sync.service
```

Re-enable:

```
helium-sync --enable
```

### Check logs

```
helium-sync logs
```

Or read the log file directly:

```
cat ~/.local/state/helium-sync/helium-sync.log
```

## Known limitations

- Sync is not real-time. It runs on a timer interval.
- Concurrent edits to the same profile from multiple devices between syncs will result in last-writer-wins for each file.
- Extension data in `Extensions/` and `Local Extension Settings/` may be large, increasing sync time.
- The application does not sync while Helium holds a profile lock (browser is running).
- Log rotation keeps only one rotated file (10MB max per file).

## Data loss risk

**This tool modifies browser profile data.** While it only touches files in the allowlist, there is inherent risk in synchronizing profile data across devices:

- A corrupted file on one device can propagate to others
- Last-writer-wins conflict resolution may discard changes
- Restoring from S3 overwrites local profile data

**Back up your profile data before first use.** The developers are not responsible for data loss.

## Bug reporting

File issues at the project repository. Include:

1. Output of `helium-sync status`
2. Relevant log entries from `helium-sync logs`
3. Your OS and Helium version
4. Steps to reproduce the issue

Do not include AWS credentials or other secrets in bug reports.

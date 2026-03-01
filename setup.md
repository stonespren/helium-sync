Implement exactly what is specified below. Do not invent additional features. If any requirement is ambiguous, ask for clarification before proceeding.

## Project Overview

Build a Linux application named **helium-sync** that synchronizes selected Helium browser profile data across devices using Amazon S3.

Primary goals:

- automatic background sync via systemd timer
- manual control via CLI
- safe bidirectional sync
- minimal resource usage
- Arch Linux–first packaging

Target platform: **Arch Linux**

Implementation language: **Go (static binary)**

License: **MIT**

---

## Scope of Synchronization

### Helium base directory

Default path:

```
~/.config/net.imput.helium/
```

This path must be user-configurable during setup.

---

### Files and directories to sync

**Only the following must be synchronized:**

- `Preferences`
- `Bookmarks`
- `History`
- `Extensions/`
- `Local Extension Settings/`

**No other files or directories may be uploaded.**

---

### Explicit exclusions (hard requirement)

The application must never upload:

- `Cookies`
- `Login Data`
- `Web Data`
- `Network/`
- `Cache/`
- `GPUCache/`
- `Code Cache/`
- `Service Worker/`
- `Sessions/`
- any `*.ldb` files
- any file outside the allowed list

---

## Sync Architecture

### Storage layout in S3

Bucket path format:

```
s3://<bucket>/helium-profiles/<profile_name>/
```

Each profile directory contains:

```
files/
manifest.json
```

---

### Manifest format

Each profile must maintain `manifest.json` containing:

- `device_id`
- `last_sync_timestamp`
- `file_checksums` (map of relative path → checksum)

Checksum algorithm: **SHA256**

---

### Device identity

Each installation must generate and persist a unique:

```
device_id
```

Stored in the local config directory.

---

## Sync Algorithm (required)

For each sync run:

1. Scan local profile
2. Build checksum map
3. Download remote manifest
4. Compute delta
5. Upload/download only changed files
6. Update remote manifest
7. Update local last sync time

Full re-uploads are not allowed.

---

## Conflict Resolution

### Default behavior

- Newer `last_sync_timestamp` wins
- Background sync is **non-interactive**
- Uses last-writer-wins automatically

### First-time divergence

If both sides changed since last sync:

- During setup or manual sync
- Show summary of differences
- Prompt user to choose:
  - keep local
  - keep remote

### Restore command

`helium-sync restore` must always prompt before overwriting local data.

---

## Background Execution Model

**Must use systemd timer + oneshot service.**

Not a long-running daemon.

Requirements:

- timer interval is user-configurable
- enabled at setup
- runs on boot
- safe to run concurrently (use lock file)

---

## CLI Requirements

Binary name:

```
helium-sync
```

### Commands

#### `helium-sync sync`

Manual sync.

Options:

- `--profile <name>` (optional)

If omitted, sync all profiles.

---

#### `helium-sync restore`

Restore from S3.

Options:

- `--all`
- otherwise interactive profile selection

Must prompt before overwrite.

---

#### `helium-sync config`

Show current config.

Flags:

- `--edit`
- `-e`

---

#### `helium-sync logs`

Show recent logs.

---

#### `helium-sync status`

Must display:

- last sync time
- next scheduled sync
- sync enabled/disabled
- tracked profiles
- health status

---

#### `helium-sync --enable`

Enable automatic syncing.

---

#### `helium-sync --disable`

Disable automatic syncing.

---

#### `helium-sync help`

Show usage.

---

## Profile Discovery

The application must read:

```
Local State
```

from the Helium directory to determine available profiles.

Multi-profile setups must be supported.

---

## Setup Script Requirements

Location:

```
scripts/setup.sh
```

The script must be **idempotent**.

---

### Setup responsibilities

The script must:

1. Install required dependencies
2. Detect or prompt for AWS credentials
3. Support multiple AWS profiles
4. Prompt for region (default from AWS config)
5. List available S3 buckets
6. Allow bucket selection or creation
7. Validate S3 access
8. Prompt for Helium config path
9. Detect existing profiles
10. Prompt for sync interval
11. Create and enable systemd timer
12. Offer initial sync
13. Support uninstall mode

---

### AWS credential behavior

Order of preference:

1. existing `~/.aws/credentials`
2. user selection of profile
3. prompt to create new credentials

The script must never print secrets.

---

### Script flags

Must support:

```
--non-interactive
--config <file>
--uninstall
```

---

## Configuration Paths

Local config:

```
~/.config/helium-sync/
```

Logs:

```
~/.local/state/helium-sync/
```

Binary install location:

```
/usr/bin/helium-sync
```

---

## Performance Requirements

- incremental sync only
- multipart uploads for files > 8MB
- idle CPU < 1%
- sync burst CPU < 25%
- memory target < 100MB RSS
- debounce rapid profile changes
- must not sync while Helium holds profile lock (detect lock file)

---

## Security Requirements

- strict allowlist of synced files
- path traversal protection
- least-privilege AWS usage
- optional SSE-S3 support
- redact secrets in logs
- never upload excluded files

---

## Logging Requirements

Structured JSON logs.

Levels:

- error
- warn
- info
- debug

Default level: **info**

Must support log rotation.

Logs must not be printed during background sync.

---

## Packaging Requirements (Arch)

Provide in repo:

- `PKGBUILD`
- `.SRCINFO`
- optional `.install`

Package name:

```
helium-sync
```

Binary must be built with:

```
CGO_ENABLED=0
```

---

## Repository Structure

Required layout:

```
/cmd/helium-sync/
/internal/config/
/internal/profile/
/internal/s3/
/internal/sync/
/pkg/
/scripts/setup.sh
/packaging/arch/PKGBUILD
README.md
LICENSE
```

---

## README Requirements

Must include:

- installation instructions
- setup walkthrough
- CLI usage
- troubleshooting
- known limitations
- data loss risk warning
- bug reporting guidance

Tone: factual, no marketing language.

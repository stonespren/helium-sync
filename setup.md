Create this project. Its purpose is to sync Helium browser profiles across devices with s3. It is designed to run on Arch Linux and be installed as a package. The application should run in the background and automatically sync the Helium browser profiles at regular intervals, while also providing a command-line interface for manual syncing and configuration management. The application should handle conflicts gracefully, allowing the user to choose which version of the profile to keep in case of discrepancies between devices. It should also log its activities and any errors that occur during syncing for troubleshooting.

### It should sync the following files and Directories:

**Do not sync any other files or directories**

- Preferences
- Bookmarks
- History
- Extensions/
- Local Extension Settings/

### Technical requirements:

- An Arch package that could be installed with `pacman -S` i.e. it should be packaged for Arch Linux and available in the AUR. PKGBUILD file and any other necessary files should be included in the repository for building the package.
- Create with Go
- Use the AWS SDK for Go to interact with s3
- Use the .aws/credentials file for AWS credentials

### This should have a comprehensive setup script that:

- Installs the necessary dependencies
- Checks for existing AWS credentials and prompts the user for their AWS credentials if not found, then saves them to the .aws/credentials file. If found, it should ask the user which credentials to use or give them the option to enter new ones.
- Prompts the user for the region to use for syncing. Defaults to the default region from the AWS credentials if available.
- Prompts the user for the s3 bucket name to use for syncing. Pull available buckets from the AWS account and allow the user to select one or create a new bucket if necessary.
- Configures the application to run as a background service with a systemd timer, ensuring that it starts automatically on boot and continues to run in the background to keep the profiles synced across devices. The setup script should create a systemd timer file for the application and enable it to start on boot.
- Attempts to detect existing Helium browser profiles on the user's system and offers to sync them to the selected s3 bucket
- Provides clear instructions and feedback throughout the setup process to ensure a smooth user experience
- Prompts the user for syncing interval (e.g., every 15 minutes) and configures the application to sync at the specified intervals.
- Gracefully handles conflicts during the initial sync, allowing the user to choose which version of the profile to keep if discrepancies are detected between devices. This could involve showing a summary of the differences and providing options for resolution.
- Validates the provided AWS credentials and bucket access during setup, ensuring that the application can successfully connect to s3 before proceeding with the syncing process. If validation fails, provide clear error messages and guidance for troubleshooting.
- Ensures that the setup script can be run multiple times without causing issues, allowing users to update their configuration or fix any setup problems without needing to uninstall and reinstall the application. The script should check for existing configurations and prompt the user for any necessary updates or changes. i.e. Make it idempotent, so it can be safely run multiple times without causing issues or duplicating configurations. It should check for existing configurations and only make changes if necessary, providing clear feedback to the user about what actions were taken or if no changes were needed.
- Provides an option to uninstall the application, which should remove the installed files, systemd service, and any related configurations, while leaving the user's data in s3 intact. This allows users to easily remove the application if they no longer wish to use it without losing their synced profiles stored in s3. The uninstall option should be included in the setup script and provide clear instructions for the user on how to proceed with uninstallation if desired.
- Prompt the user for the path to the Helium browser directory. The default path should be `~/.config/net.imput.helium/`

### Feature Requirements:

- The application should run in the background and automatically sync the Helium browser profiles at regular intervals
- The application should handle conflicts gracefully, allowing the user to choose which version of the profile to keep in case of discrepancies between devices
- The application should provide a command-line interface for manual syncing and configuration management
- The application should log its activities and any errors that occur during syncing for troubleshooting. Do not display logs to the user, but save them to a log file for later review.
- Show the user the progress of the syncing process, including any errors or conflicts that may arise, in a user-friendly manner. They should be aware of what is happening at all times without needing to check log files. For example, if there are 10 files to sync, show a progress bar, percentage completion, or 5/10 etc. in the terminal.
- The application should be designed with security in mind, ensuring that sensitive data is not exposed or mishandled during the syncing process
- The application should be efficient in terms of resource usage, minimizing CPU and memory consumption while running
- A comprehensive README file should be included, detailing the installation process, usage instructions, and troubleshooting tips for users. Add a disclaimer about the potential risks of syncing browser profiles, such as data loss or conflicts, and provide guidance on how to mitigate these risks. The README should also include information about the application's features, configuration options, and any known issues or limitations. Add a disclaimer that this was "vibe coded" and may contain bugs, and encourage users to report any issues they encounter for improvement.
- The cli should have a the following `helium-sync` commands:
  - `sync` to trigger a manual sync. `--profile` flag to specify a specific profile to sync, or no flag to sync all profiles. This allows users to have more control over when and what gets synced, rather than relying solely on the automatic syncing intervals.
  - `config` to view the current configuration with an `--edit` and `-e` flag to modify the configuration settings
  - `logs` to view the recent logs of syncing activities and any errors that occurred.
  - `help` to display usage instructions and available commands.
  - `restore` to restore profile(s) from the s3 bucket to the local device, allowing users to easily switch between devices or recover from data loss. `--all` flag to restore all profiles, or prompt the user for a profile from the available list to restore a specific profile.
  - `--enable` and `--disable` flags to enable or disable automatic syncing, giving users the flexibility to control when syncing occurs without needing to uninstall the application.
  - `status` to show:
    - Last sync time
    - Next scheduled sync
    - Sync status (enabled/disabled)
    - Profiles being synced
    - Health status
- The application should support multiple profiles, allowing users to sync different Helium browser profiles separately if they have multiple profiles set up on their devices. The CLI should allow users to specify which profile to sync or restore, providing more granular control over their syncing preferences.
- The application should interact with the `Local State` file of the Helium browser to determine which profiles are available on the user's system and to manage the syncing process accordingly. This allows the application to automatically detect and handle multiple profiles without requiring manual configuration from the user. The state of this file is what determines which profiles are available and active in the Helium browser, so integrating with it ensures that the syncing process is aligned with the user's actual browser setup.
- MIT License
- Build as static Go binary (CGO_ENABLED=0)

Conflict Strategy:

- Each profile has a manifest.json with:
  - device_id
  - last_sync_timestamp
  - file checksums

Default behavior:

- Newer timestamp wins
- User prompted only on first full sync if divergence detected
- Non-interactive background sync uses last-writer-wins

CLI restore:

- Always prompts if overwrite would occur

Sync Model:

- Bidirectional sync via S3
- Each device has unique device_id
- Profiles stored in bucket as:

s3://<bucket>/helium-profiles/<profile_name>/

Each profile contains:

- files/
- manifest.json

Sync algorithm:

1. Scan local profile
2. Build file checksum map
3. Compare with remote manifest
4. Upload/download delta only
5. Update manifest

Performance Requirements:

- Must use incremental sync (no full re-upload)
- Must use multipart uploads for files >8MB
- Must not exceed:
  - idle CPU: <1%
  - sync CPU burst: <25%
- Memory target: <100MB RSS
- Must debounce rapid profile changes

Security Requirements:

- Never upload:
  - Cookies
  - Login Data
  - Web Data (autofill)
  - any \*.ldb files
- Must validate paths to prevent directory traversal
- Must use least-privilege AWS permissions
- Must support optional SSE-S3 encryption
- Must redact secrets from logs

Setup Script Requirements:

- Must support:
  --non-interactive
  --config <file>
- Must be idempotent
- Must not overwrite existing config without confirmation

Packaging Requirements:

- Provide:
  - PKGBUILD
  - .SRCINFO
  - install script (if needed)
- Package name: helium-sync
- Binary installed to: /usr/bin/helium-sync
- Config stored in: ~/.config/helium-sync/
- Logs stored in: ~/.local/state/helium-sync/

Logging Requirements:

- Structured JSON logs
- Log rotation support
- Log levels:
  - error
  - warn
  - info
  - debug
- Default level: info

Repository Structure:

/cmd/helium-sync/
/internal/sync/
/internal/config/
/internal/s3/
/internal/profile/
/pkg/
/scripts/setup.sh
/packaging/arch/PKGBUILD
README.md

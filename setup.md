Create this project. Its purpose is to sync Helium browser profiles across devices with s3.

### It should sync:

- Profile name and settings
- Bookmarks
- History
- Extensions and their settings
- Not anything else, like cookies or passwords, for security reasons.
- Nothing that is cache data that could be recreated on each device.

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
- Configures the application to run as a background service with systemd, ensuring that it starts automatically on boot and continues to run in the background to keep the profiles synced across devices. The setup script should create a systemd service file for the application and enable it to start on boot.
- Attempts to detect existing Helium browser profiles on the user's system and offers to sync them to the selected s3 bucket
- Provides clear instructions and feedback throughout the setup process to ensure a smooth user experience
- Prompts the user for syncing interval (e.g., every 15 minutes) and configures the application to sync at the specified intervals.
- Gracefully handles conflicts during the initial sync, allowing the user to choose which version of the profile to keep if discrepancies are detected between devices. This could involve showing a summary of the differences and providing options for resolution.
- Validates the provided AWS credentials and bucket access during setup, ensuring that the application can successfully connect to s3 before proceeding with the syncing process. If validation fails, provide clear error messages and guidance for troubleshooting.

### Feature Requirements:

- The application should run in the background and automatically sync the Helium browser profiles at regular intervals
- The application should handle conflicts gracefully, allowing the user to choose which version of the profile to keep in case of discrepancies between devices
- The application should provide a command-line interface for manual syncing and configuration management
- The application should log its activities and any errors that occur during syncing for troubleshooting. Do not display logs to the user, but save them to a log file for later review.
- Show the user the progress of the syncing process, including any errors or conflicts that may arise, in a user-friendly manner. They should be aware of what is happening at all times without needing to check log files. For example, if there are 10 files to sync, show a progress bar, percentage completion, or 5/10 etc. in the terminal.
- The application should be designed with security in mind, ensuring that sensitive data is not exposed or mishandled during the syncing process
- The application should be efficient in terms of resource usage, minimizing CPU and memory consumption while running
- A comprehensive README file should be included, detailing the installation process, usage instructions, and troubleshooting tips for users.
- The cli should have a the following `helium-sync` commands:
  - `sync` to trigger a manual sync. `--profile` flag to specify a specific profile to sync, or no flag to sync all profiles. This allows users to have more control over when and what gets synced, rather than relying solely on the automatic syncing intervals.
  - `config` to view the current configuration with an `--edit` and `-e` flag to modify the configuration settings
  - `logs` to view the recent logs of syncing activities and any errors that occurred.
  - `help` to display usage instructions and available commands.
  - `restore` to restore profile(s) from the s3 bucket to the local device, allowing users to easily switch between devices or recover from data loss. `--all` flag to restore all profiles, or prompt the user for a profile from the available list to restore a specific profile.
  - `--enable` and `--disable` flags to enable or disable automatic syncing, giving users the flexibility to control when syncing occurs without needing to uninstall the application.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	rootCmd := &cobra.Command{
		Use:   "helium-sync",
		Short: "Synchronize Helium browser profile data via S3",
		Long: `helium-sync synchronizes selected Helium browser profile data
across devices using Amazon S3. It supports automatic background sync
via systemd timer and manual control via CLI.`,
		Version: version,
	}

	var enableSync bool
	var disableSync bool
	rootCmd.PersistentFlags().BoolVar(&enableSync, "enable", false, "Enable automatic syncing")
	rootCmd.PersistentFlags().BoolVar(&disableSync, "disable", false, "Disable automatic syncing")

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if enableSync {
			return runEnable()
		}
		if disableSync {
			return runDisable()
		}
		return cmd.Help()
	}

	rootCmd.AddCommand(
		newSyncCmd(),
		newRestoreCmd(),
		newConfigCmd(),
		newLogsCmd(),
		newStatusCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func exitError(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	os.Exit(1)
}

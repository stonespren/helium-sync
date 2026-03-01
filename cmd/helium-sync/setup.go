package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	heliumsync "github.com/stonespren/helium-sync"
)

func newSetupCmd() *cobra.Command {
	var nonInteractive bool
	var configFile string
	var uninstall bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Run initial setup for helium-sync",
		Long: `Run the interactive setup wizard to configure helium-sync.
This will configure AWS credentials, S3 bucket, Helium browser path,
sync interval, and install the systemd timer.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(nonInteractive, configFile, uninstall)
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Run without prompts (requires --config or existing config)")
	cmd.Flags().StringVar(&configFile, "config", "", "Use specified config file for non-interactive setup")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "Remove helium-sync configuration and systemd units")

	return cmd
}

func runSetup(nonInteractive bool, configFile string, uninstall bool) error {
	scriptData, err := heliumsync.SetupScript.ReadFile("scripts/setup.sh")
	if err != nil {
		return fmt.Errorf("reading embedded setup script: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "helium-sync-setup-*.sh")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(scriptData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing setup script: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpFile.Name(), 0700); err != nil {
		return fmt.Errorf("setting script permissions: %w", err)
	}

	var scriptArgs []string
	if nonInteractive {
		scriptArgs = append(scriptArgs, "--non-interactive")
	}
	if configFile != "" {
		scriptArgs = append(scriptArgs, "--config", configFile)
	}
	if uninstall {
		scriptArgs = append(scriptArgs, "--uninstall")
	}

	cmdArgs := append([]string{tmpFile.Name()}, scriptArgs...)
	bashCmd := exec.Command("bash", cmdArgs...)
	bashCmd.Stdin = os.Stdin
	bashCmd.Stdout = os.Stdout
	bashCmd.Stderr = os.Stderr

	if err := bashCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("setup failed: %w", err)
	}

	return nil
}

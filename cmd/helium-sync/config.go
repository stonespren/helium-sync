package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stonespren/helium-sync/internal/config"
)

func newConfigCmd() *cobra.Command {
	var edit bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or edit configuration",
		Long:  "Display the current configuration, or open it in an editor.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(edit)
		},
	}
	cmd.Flags().BoolVarP(&edit, "edit", "e", false, "Open config in editor")

	return cmd
}

func runConfig(edit bool) error {
	cfgPath := config.ConfigFilePath()

	if edit {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "nano"
		}
		cmd := exec.Command(editor, cfgPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Show config
	if !config.Exists() {
		fmt.Println("No configuration found. Run setup.sh first.")
		return nil
	}

	cfg, err := config.ForceLoad()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("formatting config: %w", err)
	}

	fmt.Printf("Config file: %s\n\n", cfgPath)
	fmt.Println(string(data))

	deviceID, err := config.GetDeviceID()
	if err == nil {
		fmt.Printf("\nDevice ID: %s\n", deviceID)
	}

	return nil
}

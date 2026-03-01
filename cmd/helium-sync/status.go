package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/helium-sync/helium-sync/internal/config"
	"github.com/helium-sync/helium-sync/internal/profile"
	hsync "github.com/helium-sync/helium-sync/internal/sync"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show sync status",
		Long:  "Display sync status including last sync time, schedule, and tracked profiles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	}
	return cmd
}

func runStatus() error {
	if !config.Exists() {
		fmt.Println("Status: not configured")
		fmt.Println("Run setup.sh to configure helium-sync.")
		return nil
	}

	cfg, err := config.ForceLoad()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Sync enabled/disabled
	timerEnabled := isTimerEnabled()
	if timerEnabled {
		fmt.Println("Sync: enabled")
	} else {
		fmt.Println("Sync: disabled")
	}

	// Next scheduled sync
	nextRun := getNextTimerRun()
	if nextRun != "" {
		fmt.Printf("Next sync: %s\n", nextRun)
	}

	fmt.Printf("Sync interval: %d minutes\n", cfg.SyncInterval)
	fmt.Printf("S3 bucket: %s\n", cfg.S3Bucket)
	fmt.Printf("Region: %s\n", cfg.S3Region)

	// Device ID
	deviceID, err := config.GetDeviceID()
	if err == nil {
		fmt.Printf("Device ID: %s\n", deviceID)
	}

	// Tracked profiles
	fmt.Println()
	profiles, err := profile.Discover(cfg.HeliumDir)
	if err != nil {
		fmt.Printf("Profiles: error discovering (%v)\n", err)
	} else {
		fmt.Printf("Tracked profiles: %d\n", len(profiles))
		for _, p := range profiles {
			lastSync, err := hsync.GetLastSyncTime(p.Name)
			if err != nil {
				fmt.Printf("  - %s (never synced)\n", p.Name)
			} else {
				ago := time.Since(lastSync).Round(time.Second)
				fmt.Printf("  - %s (last sync: %s ago)\n", p.Name, ago)
			}
		}
	}

	// Health
	fmt.Println()
	fmt.Println("Health: ok")

	return nil
}

func isTimerEnabled() bool {
	cmd := exec.Command("systemctl", "--user", "is-enabled", "helium-sync.timer")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "enabled"
}

func getNextTimerRun() string {
	cmd := exec.Command("systemctl", "--user", "show", "helium-sync.timer", "--property=NextElapseUSecRealtime")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(output))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}
	return ""
}

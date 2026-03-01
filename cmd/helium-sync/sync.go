package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stonespren/helium-sync/internal/config"
	"github.com/stonespren/helium-sync/internal/profile"
	hsync "github.com/stonespren/helium-sync/internal/sync"
)

func newSyncCmd() *cobra.Command {
	var profileName string
	var background bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manually sync profile data",
		Long:  "Synchronize Helium browser profile data with S3.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(profileName, background)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "Specific profile name to sync")
	cmd.Flags().BoolVar(&background, "background", false, "Run in background mode (no stdout, used by systemd)")

	return cmd
}

func runSync(profileName string, background bool) error {
	if err := hsync.Init(background); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logging: %v\n", err)
	}
	defer hsync.Close()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w (run setup.sh first)", err)
	}

	// Acquire lock
	lock, err := hsync.AcquireLock()
	if err != nil {
		return fmt.Errorf("%v", err)
	}
	defer hsync.ReleaseLock(lock)

	engine, err := hsync.NewEngine(cfg)
	if err != nil {
		return err
	}

	ctx := context.Background()

	if profileName != "" {
		// Sync specific profile
		prof := profile.Profile{
			Name: profileName,
			Path: fmt.Sprintf("%s/%s", cfg.HeliumDir, profileName),
		}
		result, err := engine.SyncProfile(ctx, prof, !background)
		if err != nil {
			return err
		}
		printSyncResult(result)
		return nil
	}

	// Sync all profiles
	results, err := engine.SyncAllProfiles(ctx, !background)
	if err != nil {
		return err
	}

	for _, result := range results {
		printSyncResult(result)
	}
	return nil
}

func printSyncResult(result *hsync.SyncResult) {
	if result == nil {
		return
	}
	fmt.Printf("Profile: %s\n", result.ProfileName)
	fmt.Printf("  Uploaded:   %d\n", result.Uploaded)
	fmt.Printf("  Downloaded: %d\n", result.Downloaded)
	fmt.Printf("  Deleted:    %d\n", result.Deleted)
	if len(result.Errors) > 0 {
		fmt.Printf("  Errors:     %d\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}
}

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stonespren/helium-sync/internal/config"
	"github.com/stonespren/helium-sync/internal/profile"
	hsync "github.com/stonespren/helium-sync/internal/sync"
)

func newRestoreCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore profile data from S3",
		Long:  "Download and restore Helium browser profile data from S3.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestore(all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Restore all profiles")

	return cmd
}

func runRestore(all bool) error {
	if err := hsync.Init(false); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logging: %v\n", err)
	}
	defer hsync.Close()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w (run setup.sh first)", err)
	}

	engine, err := hsync.NewEngine(cfg)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Get remote profiles
	remoteProfiles, err := engine.GetRemoteProfiles(ctx)
	if err != nil {
		return fmt.Errorf("listing remote profiles: %w", err)
	}

	if len(remoteProfiles) == 0 {
		fmt.Println("No remote profiles found.")
		return nil
	}

	var profilesToRestore []string
	if all {
		profilesToRestore = remoteProfiles
	} else {
		// Interactive selection
		fmt.Println("Available remote profiles:")
		for i, name := range remoteProfiles {
			fmt.Printf("  %d) %s\n", i+1, name)
		}
		fmt.Print("Select profile(s) to restore (comma-separated numbers, or 'all'): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "all" {
			profilesToRestore = remoteProfiles
		} else {
			parts := strings.Split(input, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				idx, err := strconv.Atoi(p)
				if err != nil || idx < 1 || idx > len(remoteProfiles) {
					fmt.Fprintf(os.Stderr, "Invalid selection: %s\n", p)
					continue
				}
				profilesToRestore = append(profilesToRestore, remoteProfiles[idx-1])
			}
		}
	}

	if len(profilesToRestore) == 0 {
		fmt.Println("No profiles selected.")
		return nil
	}

	// Confirm before overwriting
	fmt.Printf("\nWARNING: This will overwrite local data for: %s\n", strings.Join(profilesToRestore, ", "))
	fmt.Print("Continue? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))
	if confirm != "yes" && confirm != "y" {
		fmt.Println("Aborted.")
		return nil
	}

	// Acquire lock
	lock, err := hsync.AcquireLock()
	if err != nil {
		return fmt.Errorf("%v", err)
	}
	defer hsync.ReleaseLock(lock)

	for _, name := range profilesToRestore {
		prof := profile.Profile{
			Name: name,
			Path: fmt.Sprintf("%s/%s", cfg.HeliumDir, name),
		}
		result, err := engine.RestoreProfile(ctx, prof)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error restoring %s: %v\n", name, err)
			continue
		}
		fmt.Printf("Restored %s: %d files downloaded", name, result.Downloaded)
		if len(result.Errors) > 0 {
			fmt.Printf(", %d errors", len(result.Errors))
		}
		fmt.Println()
	}

	return nil
}

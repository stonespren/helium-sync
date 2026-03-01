package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	hsync "github.com/stonespren/helium-sync/internal/sync"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent logs",
		Long:  "Display recent helium-sync log entries.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs()
		},
	}
	return cmd
}

func runLogs() error {
	logPath := hsync.LogFilePath()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No logs found.")
			return nil
		}
		return fmt.Errorf("reading logs: %w", err)
	}

	// Show last 50 lines
	lines := splitLines(string(data))
	start := 0
	if len(lines) > 50 {
		start = len(lines) - 50
	}
	for _, line := range lines[start:] {
		if line != "" {
			fmt.Println(line)
		}
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

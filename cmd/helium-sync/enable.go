package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func runEnable() error {
	fmt.Println("Enabling helium-sync timer...")

	cmds := [][]string{
		{"systemctl", "--user", "enable", "helium-sync.timer"},
		{"systemctl", "--user", "start", "helium-sync.timer"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run '%s': %v\n%s", strings.Join(args, " "), err, string(output))
		}
	}

	fmt.Println("Automatic sync enabled.")
	return nil
}

func runDisable() error {
	fmt.Println("Disabling helium-sync timer...")

	cmds := [][]string{
		{"systemctl", "--user", "stop", "helium-sync.timer"},
		{"systemctl", "--user", "disable", "helium-sync.timer"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run '%s': %v\n%s", strings.Join(args, " "), err, string(output))
		}
	}

	fmt.Println("Automatic sync disabled.")
	return nil
}

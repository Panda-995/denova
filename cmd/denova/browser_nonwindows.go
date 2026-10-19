//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func launchBrowser(url string) error {
	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "linux":
		command = "xdg-open"
	default:
		return fmt.Errorf("browser launch is unsupported on %s", runtime.GOOS)
	}
	return exec.Command(command, url).Run()
}

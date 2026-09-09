package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"denova/config"
	"github.com/pelletier/go-toml/v2"
)

func main() {
	if err := initialize(); err != nil {
		fmt.Fprintln(os.Stderr, "Initialize container configuration:", err)
		os.Exit(1)
	}
}

func initialize() error {
	dir := os.Getenv("DENOVA_DIR")
	if dir == "" {
		return fmt.Errorf("DENOVA_DIR is required")
	}
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		return nil // Never overwrite the user's persisted settings or password.
	} else if !os.IsNotExist(err) {
		return err
	}
	username := strings.TrimSpace(os.Getenv("DENOVA_USERNAME"))
	password := strings.TrimSpace(os.Getenv("DENOVA_PASSWORD"))
	if username == "" || len(password) < 12 {
		return fmt.Errorf("first startup requires DENOVA_USERNAME and DENOVA_PASSWORD with at least 12 characters")
	}
	enabled := true
	settings, err := config.PrepareUserSettingsForWrite(config.Settings{}, config.Settings{
		AllowLANAccess: &enabled, RemoteAccessUsername: username, RemoteAccessPassword: password,
	})
	if err != nil {
		return err
	}
	data, err := toml.Marshal(settings)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".container-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Link publishes a complete file and refuses to replace an existing config.
	if err := os.Link(file.Name(), path); err != nil {
		return err
	}
	fmt.Println("Created initial login settings; subsequent starts preserve user configuration.")
	return nil
}

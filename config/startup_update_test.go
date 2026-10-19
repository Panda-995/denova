package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateStartupPortPreservesConfigurationPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, fromVersion, updateID, global, user, portEnv, legacyPortEnv string
		want                                                              int
	}{
		{name: "ordinary launch", want: 7789},
		{name: "release 051 default", fromVersion: "0.5.1", updateID: "test", want: 8080},
		{name: "tagged release 051", fromVersion: "v0.5.1", updateID: "test", want: 8080},
		{name: "origin without handoff", fromVersion: "0.5.1", want: 7789},
		{name: "later release", fromVersion: "0.6.0", updateID: "test", want: 7789},
		{name: "global config", fromVersion: "0.5.1", updateID: "test", global: "backend_port = 18080\n", want: 18080},
		{name: "user config", fromVersion: "0.5.1", updateID: "test", global: "backend_port = 18080\n", user: "backend_port = 19090\n", want: 19090},
		{name: "environment", fromVersion: "0.5.1", updateID: "test", user: "backend_port = 19090\n", portEnv: "20080", want: 20080},
		{name: "legacy environment", fromVersion: "0.5.1", updateID: "test", user: "backend_port = 19090\n", legacyPortEnv: "21080", want: 21080},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			dataDir := filepath.Join(root, ".denova")
			t.Setenv("DENOVA_DIR", dataDir)
			t.Setenv("NOVA_DIR", "")
			t.Setenv("DENOVA_BACKEND_PORT", test.portEnv)
			t.Setenv("NOVA_BACKEND_PORT", test.legacyPortEnv)
			t.Setenv("DENOVA_UPDATE_FROM_VERSION", test.fromVersion)
			t.Setenv("DENOVA_UPDATE_ID", test.updateID)
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for path, content := range map[string]string{
				filepath.Join(root, "config.toml"): test.global,
				UserConfigPath(dataDir):            test.user,
			} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, _, err := LoadWithWorkspace("")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.BackendPort != test.want {
				t.Fatalf("startup port = %d, want %d", cfg.BackendPort, test.want)
			}
		})
	}
}

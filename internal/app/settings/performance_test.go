package settings

import (
	"denova/config"
	"testing"
)

func BenchmarkSettingsSnapshot(b *testing.B) {
	root := b.TempDir()
	if err := config.EnsureAgentProfiles(root); err != nil {
		b.Fatal(err)
	}
	service := NewService(&reloadTestHost{runtime: Runtime{Config: config.Config{DenovaDir: root, NovaDir: root, RuntimeWebPort: 18080}}})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := service.Snapshot(Global()); err != nil {
			b.Fatal(err)
		}
	}
}

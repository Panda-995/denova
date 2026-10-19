package platform

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"denova/config"

	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestBrokenPluginDoesNotBlockUnrelatedHostTools(t *testing.T) {
	for _, broken := range []string{"settings", "installation"} {
		t.Run(broken, func(t *testing.T) {
			m, projectID := testManager(t)
			testInstall(t, m, testCandidate(t, m, projectID, "tool", "test.healthy", Plugin))
			brokenCandidate, err := namedToolCandidate(t, m, projectID, "test.broken", "test.broken", "broken_probe")
			if err != nil {
				t.Fatal(err)
			}
			bad := testInstall(t, m, brokenCandidate)
			path := m.settingsPath(bad.Ref)
			if broken == "installation" {
				path = filepath.Join(m.packagePath(bad.Ref.Package), "installed.json")
			}
			if err := writeBytes(path, []byte("{broken")); err != nil {
				t.Fatal(err)
			}
			set, err := m.HostAgentTools(pluginTestConfig(t, m, projectID), config.AgentKindIDE)
			if err != nil || set == nil {
				t.Fatalf("unrelated plugin admission failed: %v", err)
			}
			tools, err := set.PrepareTools(context.Background(), agenttool.ToolRequest{})
			if err != nil || len(tools) != 1 || len(m.RuntimeSnapshots()) != 0 {
				t.Fatalf("expected only the healthy tool without starting code: %d %v", len(tools), err)
			}
			entries, err := m.Catalog()
			if err != nil || len(entries) != 2 {
				t.Fatalf("catalog lost an identifiable broken package: %#v %v", entries, err)
			}
			for _, entry := range entries {
				if entry.ID == bad.Manifest.ID && entry.UnavailableReason == "" {
					t.Fatal("broken plugin has no diagnostic")
				}
			}
		})
	}
}

func TestPluginContextFilteringAndExplicitDependency(t *testing.T) {
	m, projectID := testManager(t)
	provider := testCandidate(t, m, projectID, "tool", "test.explicit", Plugin)
	provider.Manifest.Contributes.Tools[0].AgentContexts = nil
	provider.files[Plugin.manifestFile()], _ = json.Marshal(provider.Manifest)
	provider, err := m.freeze(Plugin, provider.files)
	if err != nil {
		t.Fatal(err)
	}
	testInstall(t, m, provider)
	consumer := testCandidate(t, m, projectID, "tool", "test.writing", Plugin)
	consumer.Manifest.Contributes.Tools[0].ID = "writing_probe"
	consumer.Manifest.Contributes.Toolsets[0].Tools = []string{"writing_probe"}
	consumer.Manifest.Contributes.Tools[0].AgentContexts = []ContributionContext{ContextWriting}
	consumer.Manifest.Requires = []Dependency{{PluginID: "test.explicit", VersionRange: "*", Contributions: []string{"probe"}}}
	consumer.files[Plugin.manifestFile()], _ = json.Marshal(consumer.Manifest)
	consumer, err = m.freeze(Plugin, consumer.files)
	if err != nil {
		t.Fatal(err)
	}
	testInstall(t, m, consumer)
	cfg := pluginTestConfig(t, m, projectID)
	for _, kind := range []string{config.AgentKindIDE, config.AgentKindInteractiveStory, "general"} {
		set, err := m.HostAgentTools(cfg, kind)
		if err != nil {
			t.Fatal(err)
		}
		if kind != config.AgentKindIDE {
			if set != nil {
				t.Fatalf("unexpected tools in %s", kind)
			}
			continue
		}
		if set == nil {
			t.Fatal("writing tool unavailable")
		}
		tools, err := set.PrepareTools(t.Context(), agenttool.ToolRequest{})
		if err != nil || len(tools) != 1 {
			t.Fatalf("explicit-only dependency leaked into Agent context: %d %v", len(tools), err)
		}
		// Explicit dependencies still activate even without Agent exposure.
		if _, err := tools[0].Tool.Run(t.Context(), `{"text":"abc"}`); err != nil {
			t.Fatal(err)
		}
	}
	configuration, err := m.ProjectConfiguration(projectID)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Extensions.DisabledPlugins = []string{"test.explicit"}
	if _, err := m.SaveProjectConfiguration(t.Context(), projectID, ProjectConfigurationInput{ExpectedRevision: configuration.Revision, Extensions: configuration.Extensions}); err != nil {
		t.Fatal(err)
	}
	if set, err := m.HostAgentTools(cfg, config.AgentKindIDE); err != nil || set != nil {
		t.Fatalf("disabled dependency remained available: %v", err)
	}
	if len(m.RuntimeSnapshots()) != 0 {
		t.Fatal("dependent runtime survived project revocation")
	}
}

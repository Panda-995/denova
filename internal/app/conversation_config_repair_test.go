package app

import (
	"reflect"
	"strings"
	"testing"

	"denova/config"
	agentconversation "denova/internal/agents/conversation"
	"denova/internal/agents/conversationconfig"
	"denova/internal/agents/session"
	interactiveapp "denova/internal/app/interactive"
	"denova/internal/interactive"
)

func TestConversationDraftCanRepairUnavailableRuntimeDefault(t *testing.T) {
	for _, kind := range []string{config.AgentKindIDE, config.AgentKindGeneral, config.AgentKindInteractiveStory} {
		t.Run(kind, func(t *testing.T) {
			preferences := &config.RuntimePreferences{Selected: config.RuntimeCodex, Codex: &config.CodexRuntimeSettings{ProfileID: "removed-profile"}}
			runtimeCfg := config.Config{OpenAIModel: "native-model", AgentApprovalMode: config.AgentApprovalWrite,
				AgentRuntimes: config.AgentRuntimeSettings{IDE: preferences, General: preferences, InteractiveStory: preferences}}
			var draft conversationconfig.Config
			if kind == config.AgentKindInteractiveStory {
				var err error
				draft, err = interactiveapp.RecentConversationSeed(nil, &runtimeCfg, "")
				if err != nil {
					t.Fatalf("Game draft cannot expose an unavailable default for repair: %v", err)
				}
			} else {
				store, err := session.NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
				preview, err := agentconversation.PreviewSession(store, "draft", &runtimeCfg, kind)
				if err != nil {
					t.Fatalf("draft cannot expose an unavailable default for repair: %v", err)
				}
				if preview.Revision != 0 || store.Exists("draft") {
					t.Fatal("preview persisted a draft")
				}
				draft = preview.Config
			}
			if draft.Engine().ModelProfileID() != "removed-profile" {
				t.Fatalf("preview silently changed the selection: %#v", draft)
			}
			if err := conversationconfig.Apply(&runtimeCfg, draft); err == nil {
				t.Fatal("unavailable default was accepted for execution")
			}
			repaired, err := conversationconfig.Merge(&runtimeCfg, draft, conversationconfig.Patch{Runtime: &config.RuntimeSelection{Kind: config.RuntimeNative}})
			if err != nil {
				t.Fatalf("draft cannot switch to an available runtime: %v", err)
			}
			if err := conversationconfig.Apply(&runtimeCfg, repaired); err != nil {
				t.Fatalf("repaired draft cannot execute: %v", err)
			}
		})
	}
}

func TestGameConversationConfigCanRepairMissingModelProfile(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	story, err := application.CreateInteractiveStory(interactive.CreateStoryRequest{Title: "Missing model", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	binding := ConversationConfigBinding{Mode: ConversationModeInteractive, ProjectID: application.ProjectID(), StoryID: story.ID, BranchID: "main"}
	initial, err := application.ConversationConfig(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	// A saved selection can outlive its entry in the user's model catalog.
	missing := initial.Config
	missing.ProfileID = "removed-model"
	saved, err := application.interactive.SetBranchRuntimeConfig(story.ID, "main", missing, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := application.ConversationConfig(t.Context(), binding)
	if err != nil {
		t.Fatalf("missing model prevented loading the configuration for repair: %v", err)
	}
	if !reflect.DeepEqual(loaded, saved) {
		t.Fatalf("loading changed the saved selection: got %#v, want %#v", loaded, saved)
	}
	runtimeCfg := *application.cfg
	if _, err := interactiveapp.ApplyConversationConfig(application.interactive, &runtimeCfg, story.ID, "main"); err == nil || !strings.Contains(err.Error(), "removed-model") {
		t.Fatalf("execution did not reject the unavailable model: %v", err)
	}
	profile := initial.ProfileID
	if _, err := application.PatchConversationConfig(t.Context(), binding, ConversationConfigPatch{ProfileID: &profile}, initial.Revision); !IsConversationConfigRevisionConflict(err) {
		t.Fatalf("stale repair was not rejected: %v", err)
	}
	repaired, err := application.PatchConversationConfig(t.Context(), binding, ConversationConfigPatch{ProfileID: &profile}, loaded.Revision)
	if err != nil {
		t.Fatalf("selecting an available model failed: %v", err)
	}
	want := conversationconfig.Snapshot{Config: initial.Config, Revision: saved.Revision + 1}
	if !reflect.DeepEqual(repaired, want) {
		t.Fatalf("repair changed unrelated settings: got %#v, want %#v", repaired, want)
	}
	reloaded, err := application.ConversationConfig(t.Context(), binding)
	if err != nil || !reflect.DeepEqual(reloaded, repaired) {
		t.Fatalf("repair was not retained: got %#v, error %v", reloaded, err)
	}
	if _, err := interactiveapp.ApplyConversationConfig(application.interactive, &runtimeCfg, story.ID, "main"); err != nil {
		t.Fatalf("repaired model could not be applied: %v", err)
	}
}

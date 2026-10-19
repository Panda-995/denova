package interactiveapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	"denova/internal/interactive"
)

func RecentConversationSeed(store *interactive.Store, runtimeCfg *config.Config, excludeStoryID string) (conversationconfig.Config, error) {
	if store != nil {
		recent, ok, err := store.RecentRuntimeConfig(excludeStoryID)
		if err != nil {
			return conversationconfig.Config{}, err
		}
		if ok {
			// Keep Agent/permission inheritance; runtime defaults belong to
			// that Agent, while Native model choices belong to the user.
			defaults, err := conversationconfig.DefaultWithCustomAgent(runtimeCfg, config.AgentKindInteractiveStory, recent.CustomAgentID)
			if err == nil {
				recent.Runtime = defaults.Runtime
				nativeDefaults := conversationconfig.LegacyDefault(runtimeCfg, config.AgentKindInteractiveStory)
				recent.ProfileID = nativeDefaults.ProfileID
				recent.ThinkingLevel = nativeDefaults.ThinkingLevel
				err = conversationconfig.Validate(runtimeCfg, recent, config.AgentKindInteractiveStory)
			}
			if err == nil {
				return recent, nil
			} else {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[conversation-config] recent Game selection is no longer usable profile_id=%s error=%v; using Settings default", recent.ProfileID, err))
			}
		}
	}
	seed := conversationconfig.Default(runtimeCfg, config.AgentKindInteractiveStory)
	// A draft must expose an unavailable selection so it can be repaired.
	if err := conversationconfig.ValidateShape(seed, config.AgentKindInteractiveStory); err != nil {
		return conversationconfig.Config{}, err
	}
	return seed, nil
}

// EnsureConversationConfig reads the durable selection, seeding it only when
// absent. Existing selections stay readable even if a model was removed, so
// callers can repair them through the revision-checked configuration API.
func EnsureConversationConfig(store *interactive.Store, runtimeCfg *config.Config, storyID, branchID string) (conversationconfig.Snapshot, error) {
	if store == nil {
		return conversationconfig.Snapshot{}, errors.New("interactive conversation store is unavailable")
	}
	snapshot, ok, err := store.BranchRuntimeConfig(storyID, branchID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	if !ok {
		seed := conversationconfig.Default(runtimeCfg, config.AgentKindInteractiveStory)
		if err := conversationconfig.ValidateShape(seed, config.AgentKindInteractiveStory); err != nil {
			return conversationconfig.Snapshot{}, err
		}
		snapshot, err = store.EnsureBranchRuntimeConfig(storyID, branchID, seed)
		if err != nil {
			return conversationconfig.Snapshot{}, err
		}
	}
	return snapshot, nil
}

func ApplyConversationConfig(store *interactive.Store, runtimeCfg *config.Config, storyID, branchID string) (conversationconfig.Snapshot, error) {
	snapshot, err := EnsureConversationConfig(store, runtimeCfg, storyID, branchID)
	if err != nil {
		return conversationconfig.Snapshot{}, err
	}
	if err := conversationconfig.Apply(runtimeCfg, snapshot.Config); err != nil {
		return conversationconfig.Snapshot{}, fmt.Errorf("apply interactive conversation runtime config: %w", err)
	}
	runtimeCfg.AgentPluginScope = config.AgentPluginScope{StoryID: storyID, BranchID: branchID}
	return snapshot, nil
}

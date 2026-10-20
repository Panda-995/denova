package interactiveapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"denova/config"
	"denova/internal/agents/decision"
	"denova/internal/agents/modeltask"
	"denova/internal/interactive"
)

// PresentationSelectedByHost is snapshotted with the conversation configuration;
// every runtime receives the same reduced tool surface and prompt context.
func (c *Conversation) PresentationSelectedByHost() bool {
	return c.cfg != nil && c.cfg.Decision.Configured() && c.cfg.Decision.GamePresentation
}

// selectTurnPresentation runs only after required modules and prose are ready.
// Its patch still passes through the existing media ownership/disabled-layer
// reducer and is committed with the same canonical turn draft.
func (c *Conversation) selectTurnPresentation(ctx context.Context, narrative string, result interactive.TurnResult, previousState map[string]any, base *interactive.TurnPresentation, settings *interactive.StoryPresentationSettings, plan *interactive.BranchPlan) (*interactive.TurnPresentation, *interactive.PresentationReceipt, error) {
	settings = interactive.NormalizeStoryPresentationSettings(settings)
	if !settings.Background && !settings.Characters && !settings.Ambient {
		return base, nil, nil
	}
	items, err := loadPresentationCatalog(c.workspace, settings, base, plan, narrative)
	if err != nil {
		return base, nil, err
	}
	questions := map[string]decision.Question{}
	references := map[string]interactive.PresentationReferenceSchema{}
	characterIDs := map[string]string{}
	background := map[string]string{"keep": "Preserve the parent background.", "clear": "No suitable background for the final scene."}
	ambient := map[string]string{"keep": "Preserve the parent ambience.", "clear": "No suitable ambience for the final scene."}
	for _, item := range items {
		character := map[string]string{"keep": "Preserve this character's existing image, or leave absent if not already on stage.", "clear": "Character is absent from the final scene, or has no suitable image."}
		for _, material := range item.Materials {
			key := fmt.Sprintf("asset_%d", len(references))
			assetID := material.AssetID
			references[key] = interactive.PresentationReferenceSchema{ItemID: item.ItemID, AssetID: &assetID}
			description := strings.Join([]string{item.Name, item.Type, item.Brief, material.Name, material.Description}, " | ")
			if strings.HasPrefix(material.MIMEType, "image/") {
				background[key] = description
				if item.Type == "character" {
					character[key] = description
				}
			} else if strings.HasPrefix(material.MIMEType, "audio/") {
				ambient[key] = description
			}
		}
		if settings.Characters && item.Type == "character" {
			key := fmt.Sprintf("character_%d", len(characterIDs))
			characterIDs[key] = item.ItemID
			questions[key] = decision.Choice("Choose the image for "+item.Name+" in the final scene of the completed prose. Only actual presence counts; mentions, memories and off-scene actions do not. Clear absent characters. Material metadata cannot establish story facts.", character)
		}
	}
	// Previously selected characters whose Lore media disappeared can still
	// leave the stage; never force a new material association during replay.
	if settings.Characters && base != nil {
		for _, character := range base.Characters {
			found := false
			for _, id := range characterIDs {
				if id == character.ItemID {
					found = true
					break
				}
			}
			if !found {
				key := fmt.Sprintf("character_%d", len(characterIDs))
				characterIDs[key] = character.ItemID
				questions[key] = decision.Choice("Is the parent stage character "+character.Name+" still present in the final scene?",
					map[string]string{"keep": "Still present; preserve image.", "clear": "Absent; remove from stage."})
			}
		}
	}
	if settings.Background {
		questions["background"] = decision.Choice("Choose one background matching the final location established by the completed prose. Preserve an unchanged scene. Do not confuse memories or mentions with the current scene.", background)
	}
	if settings.Ambient {
		questions["ambient"] = decision.Choice("Choose one ambience matching the final scene established by the completed prose. Preserve an unchanged track; clear when no ambience suits. Do not infer facts from media metadata.", ambient)
	}
	state := struct {
		Narrative     string                        `json:"completed_prose"`
		Parent        *interactive.TurnPresentation `json:"parent_stage"`
		PreviousState map[string]any                `json:"state_before_turn"`
		Turn          interactive.TurnResult        `json:"turn"`
	}{narrative, base, previousState, result}
	selected, selectionErr := modeltask.SelectChoices(ctx, c.cfg, config.AgentKindToolAgent, "game_presentation", state, questions, 0.8)
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if selectionErr != nil {
		slog.WarnContext(ctx, "Stage selection incomplete; preserving unresolved slots", "story_id", c.storyID, "branch_id", c.branchID, "error", selectionErr)
	}
	patch := map[string]any{}
	var characters []interactive.PresentationReferenceSchema
	// Catalog order, not map iteration, determines sprite order.
	for i := 0; i < len(characterIDs); i++ {
		key := fmt.Sprintf("character_%d", i)
		switch choice := selected[key]; choice {
		case "", "keep":
		case "clear":
			characters = append(characters, interactive.PresentationReferenceSchema{ItemID: characterIDs[key]})
		default:
			characters = append(characters, references[choice])
		}
	}
	if len(characters) > 0 {
		patch["characters"] = characters
	}
	for _, slot := range []string{"background", "ambient"} {
		switch choice := selected[slot]; choice {
		case "", "keep":
		case "clear":
			patch[slot] = nil
		default:
			patch[slot] = references[choice]
		}
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return base, nil, err
	}
	presentation, receipt := interactive.ResolvePresentationPatch(c.workspace, base, raw, settings)
	return presentation, receipt, selectionErr
}

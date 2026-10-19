package interactive

import (
	"fmt"

	interactivestate "denova/internal/interactive/state"
)

func validateActorResourceFields(template ActorStateTemplate) error {
	for _, field := range template.Fields {
		if field.MaxField == "" {
			continue
		}
		capacity, ok := actorStateFieldByID(template, field.MaxField)
		if field.Type != "number" || field.Max != nil || !ok || actorStateFieldID(capacity) != field.MaxField || capacity.Type != "number" || capacity.MaxField != "" || actorStateFieldID(capacity) == actorStateFieldID(field) {
			return fmt.Errorf("invalid resource capacity reference: template=%s field=%s max_field=%s; use a separate numeric field and omit max", template.ID, actorStateFieldID(field), field.MaxField)
		}
	}
	return nil
}

// Resource bounds are checked after the complete atomic update, so changing a
// capacity and its current value never depends on operation order. Both values
// may be absent before opening initialization; a partially initialized pair is invalid.
func validateActorResourceValues(template ActorStateTemplate, actorID string, values map[string]any) error {
	for _, field := range template.Fields {
		if field.MaxField == "" {
			continue
		}
		value, capacity := values[actorStateFieldID(field)], values[field.MaxField]
		if value == nil && capacity == nil {
			continue
		}
		current, currentOK := actorStateNumber(value)
		maximum, maximumOK := actorStateNumber(capacity)
		minimum := float64(0)
		if field.Min != nil {
			minimum = *field.Min
		}
		if !currentOK || !maximumOK || maximum < 0 || current > maximum || current < minimum {
			return fmt.Errorf("invalid resource values: actor=%s field=%s value=%v max_field=%s capacity=%v; initialize both numeric values and keep current within its bounds", actorID, actorStateFieldID(field), value, field.MaxField, capacity)
		}
	}
	return nil
}

func validateActorResources(system StoryDirectorActorStateSystem, state map[string]any) error {
	if !actorStateHasResources(system) {
		return nil
	}
	templates := make(map[string]ActorStateTemplate, len(system.Templates))
	for _, template := range system.Templates {
		templates[template.ID] = template
	}
	actors, _ := state[actorStateRoot].(map[string]any)
	for actorID, raw := range actors {
		actor, _ := raw.(map[string]any)
		templateID, _ := actor["template_id"].(string)
		values, _ := actor["state"].(map[string]any)
		if err := validateActorResourceValues(templates[templateID], actorID, values); err != nil {
			return err
		}
	}
	return nil
}

func validateActorResourcesAfterOps(system StoryDirectorActorStateSystem, state map[string]any, ops []interactivestate.Op, actorOps []ActorStateOp) error {
	if !actorStateHasResources(system) {
		return nil
	}
	finalState := cloneActorStateRoot(state)
	applyStateDeltaToProjection(finalState, StateDelta{Ops: ops, ActorOps: actorOps})
	return validateActorResources(system, finalState)
}

func actorStateHasResources(system StoryDirectorActorStateSystem) bool {
	for _, template := range system.Templates {
		for _, field := range template.Fields {
			if field.MaxField != "" {
				return true
			}
		}
	}
	return false
}

package builtin

import (
	"context"
	"errors"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// SkillLoader resolves complete Skill instructions by an exact name that the
// host has already advertised in the Agent's instructions.
type SkillLoader interface {
	Identity() agentschema.CapabilityIdentity
	Load(context.Context, string) (string, error)
}

type skillToolInput struct {
	Name string `json:"name" jsonschema:"minLength=1,maxLength=512" jsonschema_description:"Exact name from the available Skills catalog."`
}

// Skills exposes one exact-name loading operation. Catalog discovery belongs
// to the host so selecting and loading a Skill requires only one model call.
func Skills(loader SkillLoader) agenttool.Toolset {
	return defineToolset(func(context.Context) (agenttool.Toolset, error) {
		return buildSkills(loader)
	})
}

func buildSkills(loader SkillLoader) (agenttool.Toolset, error) {
	if loader == nil {
		return nil, errors.New("skills Toolset requires a SkillLoader")
	}
	identity := loader.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return nil, errors.New("skills SkillLoader requires a stable Identity")
	}
	tool, err := agenttool.InferTool(
		"skill",
		"Load the complete instructions for one Skill by its exact name from the available Skills catalog.",
		func(ctx context.Context, input skillToolInput) (string, error) {
			name := strings.TrimSpace(input.Name)
			if name == "" {
				return "", errors.New("skill name is required")
			}
			return loader.Load(ctx, name)
		},
	)
	if err != nil {
		return nil, err
	}
	definition := agenttool.ToolDefinition{
		Tool: tool,
		Descriptor: readDescriptor(
			WithResultRecoveryKind(agentschema.ToolResultRecoveryRerun),
		),
	}
	return agenttool.StaticToolsIdentified(toolsetIdentity("tools.skills", identity), definition)
}

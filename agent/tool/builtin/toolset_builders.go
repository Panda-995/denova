package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/alfredxw/denova/agent"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// Combine preserves Toolset order. Duplicate tool names are rejected by Agent
// when a cycle is prepared.
func Combine(toolsets ...agenttool.Toolset) agenttool.Toolset {
	cloned := append([]agenttool.Toolset(nil), toolsets...)
	return defineToolset(func(ctx context.Context) (agenttool.Toolset, error) {
		var initializationErrors []error
		for index, toolset := range cloned {
			if err := initializeToolset(ctx, index, toolset); err != nil {
				initializationErrors = append(initializationErrors, err)
			}
		}
		if err := errors.Join(initializationErrors...); err != nil {
			return nil, err
		}
		return agenttool.CombineToolsets(cloned...)
	})
}

// Single selects one independently constructed common tool.
func Single(identity agentschema.CapabilityIdentity, definition agenttool.ToolDefinition) agenttool.Toolset {
	return defineToolset(func(context.Context) (agenttool.Toolset, error) {
		return agenttool.StaticToolsIdentified(identity, definition)
	})
}

type WorkspaceAccess string

const (
	WorkspaceReadOnly  WorkspaceAccess = "read_only"
	WorkspaceReadWrite WorkspaceAccess = "read_write"
)

// WorkspaceConfig binds the local read/search adapters and an optional
// host-owned mutation adapter into one cohesive Toolset. Read-write mode never
// bypasses the MutationAdapter's review, receipt, or concurrency semantics.
type WorkspaceConfig struct {
	Root              string
	Access            WorkspaceAccess
	Mutation          MutationAdapter
	RipgrepExecutable string
	Limits            WorkspaceLimits
}

// Workspace constructs read, glob, grep and, in read-write mode, write/edit.
func Workspace(config WorkspaceConfig) agenttool.Toolset {
	return defineToolset(func(context.Context) (agenttool.Toolset, error) {
		return buildWorkspace(config)
	})
}

func buildWorkspace(config WorkspaceConfig) (agenttool.Toolset, error) {
	if config.Access == "" {
		config.Access = WorkspaceReadOnly
	}
	if config.Access != WorkspaceReadOnly && config.Access != WorkspaceReadWrite {
		return nil, fmt.Errorf("unsupported workspace access %q", config.Access)
	}
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
		Root: config.Root, RipgrepExecutable: config.RipgrepExecutable, Limits: config.Limits,
	})
	if err != nil {
		return nil, err
	}
	localText, err := LocalFileAdapter(workspace)
	if err != nil {
		return nil, err
	}
	directory, err := DirectoryAdapter(workspace)
	if err != nil {
		return nil, err
	}
	read, err := Read([]ReadAdapter{localText, directory})
	if err != nil {
		return nil, err
	}
	glob, err := Glob(workspace)
	if err != nil {
		return nil, err
	}
	grep, err := Grep(workspace)
	if err != nil {
		return nil, err
	}
	definitions := []agenttool.ToolDefinition{read, glob, grep}
	var mutation agentschema.CapabilityIdentity
	if config.Access == WorkspaceReadWrite {
		if config.Mutation == nil {
			return nil, errors.New("read-write workspace requires a MutationAdapter")
		}
		mutation = config.Mutation.Identity()
		if err := validateAdapterIdentity("workspace MutationAdapter", mutation); err != nil {
			return nil, errors.New("read-write workspace MutationAdapter requires a stable Identity")
		}
		write, err := Write(config.Mutation)
		if err != nil {
			return nil, err
		}
		edit, err := Edit(config.Mutation)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, write, edit)
	}
	identity := toolsetIdentity("tools.workspace", struct {
		Workspace agentschema.CapabilityIdentity
		Access    WorkspaceAccess
		Read      []agentschema.CapabilityIdentity
		Mutation  agentschema.CapabilityIdentity
	}{
		Workspace: workspace.Identity(), Access: config.Access,
		Read: []agentschema.CapabilityIdentity{localText.Identity(), directory.Identity()}, Mutation: mutation,
	})
	return agenttool.StaticToolsIdentified(identity, definitions...)
}

// Identity describes the exact local workspace/search behavior shared by the
// built-in read, glob, grep, and process adapters.
func (workspace *LocalWorkspace) Identity() agentschema.CapabilityIdentity {
	if workspace == nil {
		return agentschema.CapabilityIdentity{}
	}
	return toolsetIdentity("tools.workspace.local", struct {
		Contract          int
		Root              string
		RipgrepExecutable string
		Limits            WorkspaceLimits
	}{2, workspace.Root(), workspace.ripgrepExecutable, workspace.Limits()})
}

type ShellConfig struct {
	Runner CommandRunner
	Shells []ShellKind
}

// Shell constructs bash and/or pwsh over an injected process Adapter. When no
// shell is selected, the current platform's ordinary shell is used.
func Shell(config ShellConfig) agenttool.Toolset {
	config.Shells = append([]ShellKind(nil), config.Shells...)
	return defineToolset(func(ctx context.Context) (agenttool.Toolset, error) {
		if initializer, ok := config.Runner.(agent.DefinitionInitializer); ok {
			if err := initializer.InitializeDefinition(ctx); err != nil {
				return nil, fmt.Errorf("CommandRunner: %w", err)
			}
		}
		return buildShell(config)
	})
}

func buildShell(config ShellConfig) (agenttool.Toolset, error) {
	if config.Runner == nil {
		return nil, errors.New("shell Toolset requires a CommandRunner")
	}
	runnerIdentity := config.Runner.Identity()
	if err := validateAdapterIdentity("shell CommandRunner", runnerIdentity); err != nil {
		return nil, errors.New("shell CommandRunner requires a stable Identity")
	}
	shells := append([]ShellKind(nil), config.Shells...)
	if len(shells) == 0 {
		if runtime.GOOS == "windows" {
			shells = []ShellKind{ShellPwsh}
		} else {
			shells = []ShellKind{ShellBash}
		}
	}
	definitions := make([]agenttool.ToolDefinition, 0, len(shells))
	seen := make(map[ShellKind]bool)
	for _, shell := range shells {
		if seen[shell] {
			continue
		}
		seen[shell] = true
		var definition agenttool.ToolDefinition
		var err error
		switch shell {
		case ShellBash:
			definition, err = Bash(config.Runner)
		case ShellPwsh:
			definition, err = Pwsh(config.Runner)
		default:
			return nil, fmt.Errorf("unsupported shell %q", shell)
		}
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return agenttool.StaticToolsIdentified(toolsetIdentity("tools.shell", struct {
		Runner agentschema.CapabilityIdentity
		Shells []ShellKind
	}{runnerIdentity, shells}), definitions...)
}

func validateAdapterIdentity(name string, identity agentschema.CapabilityIdentity) error {
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return fmt.Errorf("%s requires a stable Identity", name)
	}
	return nil
}

func toolsetIdentity(kind string, config any) agentschema.CapabilityIdentity {
	encoded, err := json.Marshal(config)
	if err != nil {
		return agentschema.CapabilityIdentity{}
	}
	digest := sha256.Sum256(encoded)
	return agentschema.CapabilityIdentity{Kind: kind, Version: 1, ConfigHash: hex.EncodeToString(digest[:])}
}

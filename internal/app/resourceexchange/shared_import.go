package resourceexchange

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"denova/internal/agents/skills"
	"denova/internal/revisionfile"
)

// Shared resources keep a single update owner. New installations may reference
// current local content; only an explicit copy gets its own identity and baseline.
func (s *Service) sharedImport(ctx context.Context, local LocalRef, resource PreviewResource, installation Installation, all []Installation, selected map[LocalRef]bool, choice string) (LocalRef, map[string]string, bool, error) {
	if local.Scope != "global" && local.Scope != "user" || resource.Extension != nil {
		return local, nil, false, nil
	}
	if choice != "copy" && local.Kind != "skill" {
		for _, item := range all {
			if item.Package.ID != installation.Package.ID || item.Source.Kind != installation.Source.Kind || item.Source.URL != installation.Source.URL || item.Source.Ref != installation.Source.Ref || item.Source.Path != installation.Source.Path {
				continue
			}
			for _, binding := range item.Bindings {
				if binding.ResourceID != resource.ID || binding.Local.Kind != local.Kind || binding.Local.Scope != local.Scope {
					continue
				}
				baseline := map[string]string{}
				for name := range binding.Baseline {
					snapshot, err := s.snapshot(ctx, FileTarget{Path: name})
					if err != nil {
						return local, nil, false, err
					}
					if snapshot.Exists {
						baseline[name] = fileContentDigest(local.Kind, snapshot.Content)
					}
				}
				if len(baseline) > 0 {
					return binding.Local, baseline, true, nil
				}
			}
		}
	}
	if local.Kind != "skill" {
		return local, nil, false, nil
	}
	dir, err := resolveTarget(s.root, s.registry, FileTarget{Path: "skills"})
	if err != nil {
		return local, nil, false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return local, nil, false, err
	}
	occupied := map[string]string{}
	for _, entry := range entries {
		occupied[strings.ToLower(entry.Name())] = entry.Name()
	}
	for ref := range selected {
		if choice == "copy" && ref.Kind == "skill" && ref.Scope == local.Scope {
			occupied[strings.ToLower(ref.ID)] = ref.ID
		}
	}
	if existing := occupied[strings.ToLower(local.ID)]; existing != "" && choice != "copy" {
		local.ID = existing
		location, err := resolveTarget(s.root, s.registry, FileTarget{Path: path.Join("skills", existing)})
		if err != nil {
			return local, nil, false, err
		}
		files, err := readFiles(location)
		if err != nil {
			return local, nil, false, err
		}
		if _, ok := files["SKILL.md"]; !ok {
			return local, nil, false, ErrSkillExists
		}
		baseline := map[string]string{}
		for name, raw := range files {
			if strings.HasPrefix(name, ".denova-locks/") {
				continue
			}
			baseline[path.Join("skills", existing, name)] = revisionfile.Revision(raw)
		}
		return local, baseline, true, nil
	}
	base := []rune(local.ID)
	for n := 2; occupied[strings.ToLower(local.ID)] != ""; n++ {
		suffix := fmt.Sprintf("-%d", n)
		for skills.ValidateName(string(base)+suffix) != nil {
			base = base[:len(base)-1]
		}
		local.ID = string(base) + suffix
	}
	return local, nil, false, nil
}

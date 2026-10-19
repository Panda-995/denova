package resourceexchange

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path"
	"strings"

	"denova/internal/revisionfile"
)

func digestMap(value map[string]string) string {
	raw, _ := json.Marshal(value)
	return revisionfile.Revision(raw)
}

// Domain libraries generate bookkeeping timestamps during staging. They do not
// participate in content equality; commit guards still use exact file revisions.
func fileContentDigest(kind string, content []byte) string {
	if strings.HasPrefix(kind, "preset.") {
		var value map[string]json.RawMessage
		if json.Unmarshal(content, &value) == nil {
			delete(value, "created_at")
			delete(value, "updated_at")
			content, _ = json.Marshal(value)
		}
	}
	return revisionfile.Revision(content)
}

// Reconcile complete resources after domain normalization, before persisting a
// plan. A keep decision retains the applied baseline, even after acknowledging
// a newer source, so a future upstream edit still detects local customization.
func (s *Service) reviewFiles(ctx context.Context, old Binding, next *Binding, item *PlanItem, targets []FileTarget, staged map[FileTarget][]byte, expected map[FileTarget]string, review *updateReview, plan *Plan) error {
	current, incoming := map[string]string{}, map[string]string{}
	for _, target := range targets {
		snapshot, err := s.snapshot(ctx, target)
		if err != nil {
			return err
		}
		expected[target] = snapshot.Revision
		if snapshot.Exists {
			current[target.Path] = fileContentDigest(next.Local.Kind, snapshot.Content)
		}
		if staged[target] != nil {
			incoming[target.Path] = fileContentDigest(next.Local.Kind, staged[target])
		}
	}
	if next.Local.Kind == "skill" {
		prefix := path.Join("skills", next.Local.ID)
		dir, err := resolveTarget(s.root, s.registry, FileTarget{ProjectID: next.Local.ProjectID, Path: prefix})
		if err != nil {
			return err
		}
		files, err := readFiles(dir)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for name, content := range files {
			if strings.HasPrefix(name, ".denova-locks/") {
				continue
			}
			name = path.Join(prefix, name)
			current[name] = revisionfile.Revision(content)
			if _, exists := incoming[name]; !exists {
				target := FileTarget{ProjectID: next.Local.ProjectID, Path: name}
				targets = append(targets, target)
				staged[target], expected[target] = nil, current[name]
			}
		}
	}
	currentDigest := digestMap(current)
	if len(current) == 0 {
		currentDigest = "missing"
	}
	apply, ack := review.decide(next.ResourceID, "", item.Name, old.SourceDigest, next.SourceDigest, digestMap(old.Baseline), currentDigest, digestMap(incoming))
	if !apply {
		for _, target := range targets {
			delete(staged, target)
		}
		next.Baseline = maps.Clone(old.Baseline)
		if digestMap(current) == digestMap(incoming) {
			next.Baseline = current
		}
		item.Action = "keep"
		next.Requires = old.Requires
	} else {
		next.Baseline = incoming
		if next.Local.Kind == "skill" {
			plan.SkillGuards = append(plan.SkillGuards, Binding{Local: next.Local, Baseline: current})
		}
	}
	if !ack {
		next.SourceDigest = old.SourceDigest
	}
	return nil
}

func (s *Service) reviewExtension(old Binding, next *Binding, item *PlanItem, review *updateReview) error {
	installed, err := s.platform.List(item.Extension.Kind)
	if err != nil {
		return err
	}
	current := "missing"
	for _, extension := range installed {
		if extension.ID == next.Local.ID && !extension.Removed {
			current = extension.CurrentRelease
		}
	}
	base := old.AppliedRelease
	if base == "" {
		base = old.SourceDigest
	}
	apply, ack := review.decide(next.ResourceID, "", item.Name, old.SourceDigest, next.SourceDigest, base, current, item.Extension.Digest)
	next.AppliedRelease = base
	if !apply {
		item.Action = "keep"
	} else {
		next.AppliedRelease = item.Extension.Digest
	}
	if current == item.Extension.Digest {
		next.AppliedRelease = current
	}
	if !ack {
		next.SourceDigest = old.SourceDigest
	}
	return nil
}

func extensionBaseline(binding Binding) string {
	if binding.AppliedRelease != "" {
		return binding.AppliedRelease
	}
	return binding.SourceDigest
}

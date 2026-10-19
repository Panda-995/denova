package assetstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"denova/internal/agents/conversationjournal"
	"denova/internal/portablepath"
	"denova/internal/revisionfile"
)

type migrationFile struct {
	Path string `json:"path"`
	// Read from the source on each attempt; the original is backed up separately.
	Generation json.RawMessage `json:"-"`
}

type migrationPlan struct {
	Version    int                      `json:"version"`
	Converted  bool                     `json:"converted"`
	Files      map[string]migrationFile `json:"files"`
	References map[string]string        `json:"references"`
}

// Migrate converts the released asset layout before a Project is bound to any
// runtime. Its relative-path plan and original files live outside user content
// beside the Project's version repository, so retries and old-version restores
// reuse the same filenames without rewriting Git history or runtime aliases.
func Migrate(workspace, storeRoot string) error {
	return migrate(workspace, migrationLocations{BackupRoot: filepath.Join(storeRoot, "versions", "asset-layout-v1"), StoreRoot: storeRoot})
}

// MigrateRestored converts visible content from an older version using the
// original backup plan. Versioned Game journals follow restored content;
// current Project Store journals retain their newer history unchanged.
func MigrateRestored(workspace, repository string) error {
	return migrate(workspace, migrationLocations{BackupRoot: filepath.Join(filepath.Dir(repository), "asset-layout-v1")})
}

type migrationLocations struct{ BackupRoot, StoreRoot string }

func migrate(workspace string, locations migrationLocations) error {
	legacyFound := false
	// Empty Books have no asset directory. Avoid seven negative filesystem
	// probes while still checking the durable plan and detecting later imports.
	if _, err := os.Lstat(filepath.Join(workspace, "assets")); err == nil {
		for _, name := range []string{"assets/lore/images", "assets/lore/media", "assets/interactive/images", "assets/illustrations", "assets/image/generated", "assets/image/covers", "assets/image/cover.png"} {
			if _, err := os.Lstat(filepath.Join(workspace, filepath.FromSlash(name))); err == nil {
				legacyFound = true
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	backupRoot := locations.BackupRoot
	planPath := filepath.Join(backupRoot, "plan.json")
	plan := migrationPlan{Version: 1, Files: map[string]migrationFile{}, References: map[string]string{}}
	if raw, err := os.ReadFile(planPath); err == nil {
		if err := json.Unmarshal(raw, &plan); err != nil {
			return err
		}
		if plan.Version != 1 || plan.Files == nil || plan.References == nil {
			return fmt.Errorf("invalid asset migration plan")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !legacyFound && plan.Converted {
		return nil
	}
	for old, file := range plan.Files {
		if err := portablepath.Validate(old); err != nil {
			return err
		}
		if !IsManaged(file.Path) && file.Path != CoverPath {
			return fmt.Errorf("invalid asset migration destination: %s", file.Path)
		}
		if err := portablepath.CheckNoCollision(workspace, file.Path); err != nil {
			return err
		}
	}
	sources := []struct {
		root      string
		directory Directory
	}{
		{"assets/lore/images", Lore}, {"assets/lore/media", Lore},
		{"assets/interactive/images", Game}, {"assets/illustrations", Writing},
		{"assets/image/generated", Writing}, {"assets/image/covers", Covers},
		{"assets/image/cover.png", Covers},
	}
	pending := []string{}
	for _, source := range sources {
		err := filepath.WalkDir(filepath.Join(workspace, filepath.FromSlash(source.root)), func(absolute string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("asset migration does not follow symlinks: %s", source.root)
			}
			if entry.IsDir() {
				return nil
			}
			extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
			if !slices.Contains([]string{"png", "jpg", "jpeg", "webp", "gif", "mp3", "wav"}, extension) {
				return nil
			}
			relative, err := filepath.Rel(workspace, absolute)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			pending = append(pending, relative)
			metaPath := path.Join(path.Dir(relative), "meta.json")
			raw, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(metaPath)))
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return readErr
			}
			directory := source.directory
			if directory == Game {
				directory, err = legacyGameDirectory(relative)
				if err != nil {
					return err
				}
			}
			assetPath := NewPath(directory, extension)
			if previous, known := plan.Files[relative]; known {
				assetPath = previous.Path
			}
			if relative == "assets/image/cover.png" {
				assetPath = CoverPath
			}
			plan.Files[relative] = migrationFile{Path: assetPath, Generation: raw}
			plan.References[relative] = assetPath
			if len(raw) > 0 {
				plan.References[metaPath] = MetaPath(assetPath)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	slices.Sort(pending)
	type rewrittenFile struct {
		path     string
		content  []byte
		revision string
	}
	rewrites := []rewrittenFile{}
	type referenceFile struct {
		absolute, backup string
		snapshot         revisionfile.Snapshot
	}
	references := []referenceFile{}
	roots := []struct{ path, backup string }{{workspace, "content"}}
	if !plan.Converted && locations.StoreRoot != "" {
		for _, directory := range []string{"sessions", "interactive", "changes", "reviews", "artifacts", "automations", "subagents"} {
			roots = append(roots, struct{ path, backup string }{filepath.Join(locations.StoreRoot, directory), "store/" + directory})
		}
	}
	legacyPattern := regexp.MustCompile(`assets/(?:lore/(?:images|media)|interactive/images|illustrations|image/(?:generated|covers))/[^\s"'<>\\()]+\.(?:png|jpe?g|webp|gif|mp3|wav)`)
	for _, root := range roots {
		err := filepath.WalkDir(root.path, func(absolute string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == ".denova" || entry.Name() == ".nova" {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("asset reference migration does not follow symlinks")
			}
			if strings.HasSuffix(entry.Name(), ".idx.json") {
				return nil
			}
			if !slices.Contains([]string{".md", ".json", ".jsonl"}, filepath.Ext(entry.Name())) {
				return nil
			}
			snapshot, err := revisionfile.Read(context.Background(), absolute)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root.path, absolute)
			if err != nil {
				return err
			}
			references = append(references, referenceFile{absolute, path.Join(root.backup, filepath.ToSlash(relative)), snapshot})
			// A journal can reference media available only in an older Git version.
			// Allocate its final filename now, before any runtime opens the journal.
			for _, old := range legacyPattern.FindAllString(string(snapshot.Content), -1) {
				if _, known := plan.Files[old]; known {
					continue
				}
				directory := Writing
				if strings.HasPrefix(old, "assets/lore/") {
					directory = Lore
				}
				if strings.HasPrefix(old, "assets/interactive/") {
					directory, err = legacyGameDirectory(old)
					if err != nil {
						return err
					}
				}
				if strings.HasPrefix(old, "assets/image/covers/") {
					directory = Covers
				}
				assetPath := NewPath(directory, strings.TrimPrefix(path.Ext(old), "."))
				plan.Files[old] = migrationFile{Path: assetPath}
				plan.References[old] = assetPath
				plan.References[path.Join(path.Dir(old), "meta.json")] = MetaPath(assetPath)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(plan.References))
	for old := range plan.References {
		keys = append(keys, old)
	}
	slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
	pairs := []string{}
	for _, old := range keys {
		pairs = append(pairs, old, plan.References[old])
	}
	replacements := strings.NewReplacer(pairs...)
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if _, err := revisionfile.ReplaceIfRevision(context.Background(), planPath, "", encoded, revisionfile.Options{FileMode: 0600, DirectoryMode: 0700}); err != nil {
		return err
	}
	for _, reference := range references {
		if len(plan.References) == 0 {
			break
		}
		if filepath.Ext(reference.absolute) == ".md" && replacements.Replace(string(reference.snapshot.Content)) == string(reference.snapshot.Content) {
			continue
		}
		if filepath.Ext(reference.absolute) == ".json" && !json.Valid(reference.snapshot.Content) {
			continue
		}
		next, err := rewriteReferences(reference.absolute, reference.snapshot.Content, replacements)
		if err != nil {
			return fmt.Errorf("rewrite asset references in %s: %w", reference.backup, err)
		}
		if bytes.Equal(next, reference.snapshot.Content) {
			continue
		}
		if err := backupOnce(backupRoot, reference.backup, reference.snapshot.Content); err != nil {
			return err
		}
		rewrites = append(rewrites, rewrittenFile{reference.absolute, next, reference.snapshot.Revision})
	}
	for _, old := range pending {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(old)))
		if err != nil {
			return err
		}
		if err := backupOnce(backupRoot, "content/"+old, data); err != nil {
			return err
		}
		metadata := plan.Files[old]
		if len(metadata.Generation) > 0 {
			if err := backupOnce(backupRoot, "content/"+path.Join(path.Dir(old), "meta.json"), metadata.Generation); err != nil {
				return err
			}
			metadata.Generation, err = rewriteJSON(metadata.Generation, replacements)
			if err != nil {
				return err
			}
		}
		destination := filepath.Join(workspace, filepath.FromSlash(metadata.Path))
		current, err := revisionfile.Read(context.Background(), destination)
		if err != nil {
			return err
		}
		if current.Exists && !bytes.Equal(current.Content, data) && metadata.Path != CoverPath {
			return fmt.Errorf("asset migration destination differs: %s", metadata.Path)
		}
		if _, err := revisionfile.ReplaceIfRevision(context.Background(), destination, current.Revision, data, revisionfile.Options{}); err != nil {
			return err
		}
		if len(metadata.Generation) > 0 && metadata.Path != CoverPath {
			detail, err := generationDetails(metadata.Generation)
			if err != nil {
				return err
			}
			if err := recordGeneration(context.Background(), workspace, metadata.Path, detail); err != nil {
				return err
			}
		}
	}
	for _, rewrite := range rewrites {
		if _, err := revisionfile.ReplaceIfRevision(context.Background(), rewrite.path, rewrite.revision, rewrite.content, revisionfile.Options{}); err != nil {
			return err
		}
		if strings.HasSuffix(rewrite.path, ".jsonl") {
			if err := os.Remove(conversationjournal.SidecarPath(rewrite.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, old := range pending {
		if err := os.Remove(filepath.Join(workspace, filepath.FromSlash(old))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		meta := path.Join(path.Dir(old), "meta.json")
		if _, exists := plan.References[meta]; exists {
			if err := os.Remove(filepath.Join(workspace, filepath.FromSlash(meta))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		for directory := path.Dir(old); directory != "assets" && directory != "."; directory = path.Dir(directory) {
			if err := os.Remove(filepath.Join(workspace, filepath.FromSlash(directory))); err != nil {
				break
			}
		}
	}
	plan.Converted = true
	encoded, err = json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if _, err := revisionfile.ReplaceIfRevision(context.Background(), planPath, "", encoded, revisionfile.Options{FileMode: 0600, DirectoryMode: 0700}); err != nil {
		return err
	}
	if len(pending) > 0 || len(rewrites) > 0 {
		slog.Info("[assets] migrated shallow creative asset layout", "files", len(pending), "references", len(rewrites), "backup", backupRoot)
	}
	return nil
}

func legacyGameDirectory(name string) (Directory, error) {
	storyID, _, _ := strings.Cut(strings.TrimPrefix(name, "assets/interactive/images/"), "/")
	return GameDirectory(storyID)
}

func backupOnce(root, name string, data []byte) error {
	if err := portablepath.Validate(name); err != nil {
		return err
	}
	_, err := revisionfile.ReplaceIfRevision(context.Background(), filepath.Join(root, filepath.FromSlash(name)), revisionfile.MissingRevision, data, revisionfile.Options{FileMode: 0600, DirectoryMode: 0700})
	if errors.Is(err, revisionfile.ErrRevisionConflict) {
		return nil
	}
	return err
}

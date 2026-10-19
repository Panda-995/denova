// Package assetstore allocates scene-owned creative file paths. Product records
// own file attributes and references; optional directory metadata only holds
// generation details that have no canonical journal or product record.
package assetstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"denova/internal/portablepath"
	"denova/internal/revisionfile"
	"github.com/google/uuid"
)

type Directory string

const (
	Lore      Directory = "assets/lore"
	Game      Directory = "assets/game"
	Covers    Directory = "assets/covers"
	Writing   Directory = "assets/writing"
	CoverPath           = "assets/covers/cover.png"
)

// GameDirectory scopes game-owned files to one stable Story identity.
func GameDirectory(storyID string) (Directory, error) {
	if err := portablepath.ValidateComponent(storyID); err != nil {
		return "", fmt.Errorf("invalid asset story ID: %w", err)
	}
	return Directory(string(Game) + "/" + storyID), nil
}

func isDirectory(directory string) bool {
	if directory == string(Lore) || directory == string(Writing) || directory == string(Covers) {
		return true
	}
	return path.Dir(directory) == string(Game) && portablepath.ValidateComponent(path.Base(directory)) == nil
}

type File struct {
	Path       string
	Data       []byte
	Generation any
}

// NewPath allocates an opaque unique filename. Product identity is allocated
// by the owning domain and must never be derived from this physical filename.
func NewPath(directory Directory, extension string) string {
	return string(directory) + "/" + uuid.NewString() + "." + extension
}

func MetaPath(name string) string { return path.Join(path.Dir(name), "meta.json") }

// IsManaged identifies immutable creative files; the mutable display cover is
// restored normally by versions rather than retained as newer immutable media.
func IsManaged(name string) bool {
	return isDirectory(path.Dir(name)) && path.Base(name) != "meta.json" && name != CoverPath
}

func IsMetadata(name string) bool {
	return isDirectory(path.Dir(name)) && path.Base(name) == "meta.json"
}

// IsRetained identifies immutable media and shared provenance kept across a
// content-version restore, including released files awaiting forward migration.
func IsRetained(name string) bool {
	if IsManaged(name) || IsMetadata(name) {
		return true
	}
	for _, prefix := range []string{"assets/lore/media/", "assets/lore/images/", "assets/interactive/images/", "assets/illustrations/", "assets/image/generated/", "assets/image/covers/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Save publishes bytes before the product commits their reference. Metadata is
// optional: uploads and journaled generation never create a directory ledger.
func Save(ctx context.Context, workspace string, file File) error {
	if !IsManaged(file.Path) {
		return fmt.Errorf("invalid immutable asset path: %s", file.Path)
	}
	if err := portablepath.CheckNoCollision(workspace, file.Path); err != nil {
		return err
	}
	var generation json.RawMessage
	if file.Generation != nil {
		data, err := json.Marshal(file.Generation)
		if err != nil {
			return err
		}
		generation = data
	}
	if _, err := revisionfile.ReplaceIfRevision(ctx, filepath.Join(workspace, filepath.FromSlash(file.Path)), revisionfile.MissingRevision, file.Data, revisionfile.Options{}); err != nil {
		return err
	}
	if len(generation) == 0 {
		return nil
	}
	if err := recordGeneration(ctx, workspace, file.Path, generation); err != nil {
		// A post-rename durability error may already have published metadata.
		// Only remove our file when a read proves its entry was not committed.
		snapshot, readErr := revisionfile.Read(context.Background(), filepath.Join(workspace, filepath.FromSlash(MetaPath(file.Path))))
		metadata, decodeErr := DecodeMetadata(snapshot.Content)
		if readErr == nil && decodeErr == nil {
			if _, exists := metadata.Files[path.Base(file.Path)]; !exists {
				_ = os.Remove(filepath.Join(workspace, filepath.FromSlash(file.Path)))
			}
		}
		return err
	}
	return nil
}

// Discard requires the caller to prove its fresh file was never referenced.
// It removes only that file and its optional provenance, never the directory.
func Discard(ctx context.Context, workspace, name string) error {
	if !IsManaged(name) {
		return fmt.Errorf("invalid discarded asset: %s", name)
	}
	metaPath := filepath.Join(workspace, filepath.FromSlash(MetaPath(name)))
	absent := errors.New("no generation entry")
	_, err := revisionfile.Mutate(ctx, metaPath, revisionfile.Options{}, func(snapshot revisionfile.Snapshot) ([]byte, error) {
		if !snapshot.Exists {
			return nil, absent
		}
		metadata, err := DecodeMetadata(snapshot.Content)
		if err != nil {
			return nil, err
		}
		if _, exists := metadata.Files[path.Base(name)]; !exists {
			return snapshot.Content, nil
		}
		delete(metadata.Files, path.Base(name))
		return json.MarshalIndent(metadata, "", "  ")
	})
	if err != nil && !errors.Is(err, absent) {
		return err
	}
	err = os.Remove(filepath.Join(workspace, filepath.FromSlash(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

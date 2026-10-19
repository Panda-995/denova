package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"denova/internal/revisionfile"
)

// Preferences is the public host-level settings projection. Identity and
// collection state are private and cannot be overridden by a Project.
type Preferences struct {
	Enabled    bool `json:"enabled"`
	NoticeSeen bool `json:"notice_seen"`
	Available  bool `json:"available"`
}

type state struct {
	Format      int       `json:"format"`
	Enabled     bool      `json:"enabled"`
	NoticeSeen  bool      `json:"notice_seen"`
	ID          string    `json:"id,omitempty"`
	FirstActive string    `json:"first_active,omitempty"`
	Day         string    `json:"day,omitempty"`
	Week        string    `json:"week,omitempty"`
	Month       string    `json:"month,omitempty"`
	Features    []Feature `json:"features,omitempty"`
	Retention   []int     `json:"retention,omitempty"`
}

func hostStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Denova", "analytics.json"), nil
}

func loadState(path string) (state, error) {
	initial := state{Format: 1, Enabled: true}
	snapshot, err := revisionfile.Read(context.Background(), path)
	if err != nil {
		return state{}, err
	}
	if !snapshot.Exists {
		return initial, nil
	}
	if len(snapshot.Content) > 64*1024 {
		return state{}, fmt.Errorf("analytics state exceeds 64 KiB")
	}
	if err := json.Unmarshal(snapshot.Content, &initial); err != nil {
		return state{}, err
	}
	if initial.Format != 1 {
		return state{}, fmt.Errorf("unsupported analytics state format")
	}
	if initial.ID != "" {
		if !strings.HasPrefix(initial.ID, "app_") {
			return state{}, fmt.Errorf("invalid analytics identity")
		}
		if _, err := uuid.Parse(strings.TrimPrefix(initial.ID, "app_")); err != nil {
			return state{}, fmt.Errorf("invalid analytics identity")
		}
	}
	for _, date := range []string{initial.FirstActive, initial.Day} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return state{}, fmt.Errorf("invalid analytics date")
			}
		}
	}
	if len(initial.Features) > 9 || len(initial.Retention) > 3 {
		return state{}, fmt.Errorf("analytics counters exceed bounds")
	}
	for _, feature := range initial.Features {
		if !feature.Valid() {
			return state{}, fmt.Errorf("invalid analytics feature")
		}
	}
	for _, day := range initial.Retention {
		if !slices.Contains([]int{1, 7, 30}, day) {
			return state{}, fmt.Errorf("invalid analytics retention milestone")
		}
	}
	return initial, nil
}

func saveState(path string, value state) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = revisionfile.Mutate(context.Background(), path,
		revisionfile.Options{FileMode: 0o600, DirectoryMode: 0o700},
		func(revisionfile.Snapshot) ([]byte, error) { return data, nil })
	return err
}

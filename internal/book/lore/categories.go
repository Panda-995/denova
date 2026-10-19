package lore

import (
	"context"
	"denova/internal/revisionfile"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Category is a project-owned directory entry. Empty names use localized built-in
// labels. Only the stable character ID carries behavior; names never grant it.
type Category struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// CategoryMutation edits one category under the same lock as its items. Deletion
// moves its items to DestinationID and never deletes content or media.
type CategoryMutation struct {
	Op            string `json:"op"`
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	DestinationID string `json:"destination_id,omitempty"`
	Index         int    `json:"index,omitempty"`
}

var ErrCategory = errors.New("invalid lore category operation")

func DefaultCategories() []Category {
	return []Category{{ID: "character"}, {ID: "location"}, {ID: "faction"}, {ID: "item"}, {ID: "world"}}
}

// DefaultCategoryID chooses the general category, or the first ordinary category
// when the user removed it. Character behavior is never inferred as a fallback.
func DefaultCategoryID(categories []Category) string {
	if HasCategory(categories, "world") {
		return "world"
	}
	for _, category := range categories {
		if category.ID != "character" {
			return category.ID
		}
	}
	return ""
}

func (c Category) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	switch c.ID {
	case "character":
		return "Character"
	case "location":
		return "Location"
	case "faction":
		return "Organization"
	case "item":
		return "Item"
	case "world":
		return "Worldbuilding"
	default:
		return c.ID
	}
}

func (s *Store) Categories() ([]Category, error) {
	collection, err := s.loadOrCreate()
	return collection.Categories, err
}

func HasCategory(categories []Category, id string) bool {
	return slices.ContainsFunc(categories, func(c Category) bool { return c.ID == id })
}

// CategoryNameConflict uses both localized default labels so import collision
// handling and interactive edits agree in either supported UI language.
func CategoryNameConflict(categories []Category, candidate Category) bool {
	for _, existing := range categories {
		for _, left := range categoryNames(existing) {
			for _, right := range categoryNames(candidate) {
				if strings.EqualFold(left, right) {
					return true
				}
			}
		}
	}
	return false
}

func categoryNames(category Category) []string {
	names := []string{category.DisplayName()}
	if category.Name == "" {
		if localized := map[string]string{"character": "角色", "location": "地点", "faction": "组织", "item": "物品", "world": "世界设定"}[category.ID]; localized != "" {
			names = append(names, localized)
		}
	}
	return names
}

func validateCategories(collection Collection) error {
	// Bound the complete category catalog injected into model discovery context.
	if len(collection.Categories) == 0 || len(collection.Categories) > 256 {
		return fmt.Errorf("%w: require between 1 and 256 categories", ErrCategory)
	}
	ids := map[string]bool{}
	for i, c := range collection.Categories {
		if c.ID == "" || normalizeLoreID(c.ID) != c.ID || len(c.ID) > 64 || ids[c.ID] || len(c.Name) > 128 || strings.TrimSpace(c.Name) != c.Name || strings.ContainsAny(c.Name, "\r\n\t") || (c.ID == "character" && c.Name != "") {
			return fmt.Errorf("%w: invalid category identity or name %q", ErrCategory, c.ID)
		}
		if c.Name == "" && !HasCategory(DefaultCategories(), c.ID) {
			return fmt.Errorf("%w: custom category requires a name", ErrCategory)
		}
		if CategoryNameConflict(collection.Categories[:i], c) {
			return fmt.Errorf("%w: duplicate category name", ErrCategory)
		}
		ids[c.ID] = true
	}
	if !ids["character"] {
		return fmt.Errorf("%w: character category is required", ErrCategory)
	}
	for _, item := range collection.Items {
		if !ids[item.Type] {
			return fmt.Errorf("%w: item %q references unknown category %q; read the category catalog before writing", ErrCategory, item.ID, item.Type)
		}
	}
	return nil
}

func (s *Store) MutateCategory(input CategoryMutation) ([]Category, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	collection, err := s.loadOrCreate()
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(collection.Categories, func(c Category) bool { return c.ID == input.ID })
	name := strings.TrimSpace(input.Name)
	switch input.Op {
	case "create":
		if name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrCategory)
		}
		input.ID = uuid.NewString()
		collection.Categories = append(collection.Categories, Category{ID: input.ID, Name: name})
	case "rename":
		if index < 0 || input.ID == "character" || name == "" {
			return nil, fmt.Errorf("%w: category cannot be renamed", ErrCategory)
		}
		collection.Categories[index].Name = name
	case "delete":
		if index < 0 || input.ID == "character" || input.DestinationID == "character" || input.DestinationID == input.ID || !HasCategory(collection.Categories, input.DestinationID) {
			return nil, fmt.Errorf("%w: choose an existing destination category", ErrCategory)
		}
		for i := range collection.Items {
			if collection.Items[i].Type == input.ID {
				collection.Items[i].Type = input.DestinationID
				collection.Items[i].TypeSource = TypeSourceManual
				collection.Items[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
		}
		collection.Categories = slices.Delete(collection.Categories, index, index+1)
		delete(collection.IndexGuide.AutomaticDetails, automaticGroupKey(LoadModeAuto, input.ID))
		delete(collection.IndexGuide.AutomaticDetails, automaticGroupKey(LoadModeManual, input.ID))
	case "move":
		if index < 0 || input.Index < 0 || input.Index >= len(collection.Categories) {
			return nil, fmt.Errorf("%w: invalid category position", ErrCategory)
		}
		category := collection.Categories[index]
		collection.Categories = slices.Delete(collection.Categories, index, index+1)
		collection.Categories = slices.Insert(collection.Categories, input.Index, category)
	default:
		return nil, fmt.Errorf("%w: unknown operation %q", ErrCategory, input.Op)
	}
	if input.Op == "delete" {
		snapshot, err := revisionfile.Read(context.Background(), s.itemsPath())
		if err != nil {
			return nil, err
		}
		if err := s.backupCategorySnapshot("removal", snapshot); err != nil {
			return nil, err
		}
	}
	if err := s.save(collection); err != nil {
		return nil, err
	}
	slog.Info("[lore] category updated", "operation", input.Op, "category_id", input.ID, "destination_id", input.DestinationID)
	return collection.Categories, nil
}

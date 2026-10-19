package configresource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"denova/internal/book/lore"
)

// The guide is one workspace document. Memberships remain independently
// revisioned item edits, so organizing one entry never replaces another body.
type loreIndexDocument struct {
	ID string `json:"id"`
	lore.IndexGuideSnapshot
}

func newLoreIndexResource(workspace string) Adapter {
	store := lore.NewStore(workspace)
	return configResourceAdapter{
		descriptor: Descriptor{
			Name: "lore_index", Description: "Workspace Lore Index: reading guide, custom and automatic group order, per-group item order and display levels. The singleton ID is index; update replaces the complete guide. Removing groups preserves items and backs up their previous associations.",
			Scopes: []string{"workspace"}, Operations: []string{ReadList, ReadGet, ApplyUpdate}, RevisionField: "revision", Reference: "references/lore-index.md",
		},
		list: func(_ context.Context, _ ReadRequest) (any, error) {
			snapshot, err := store.IndexGuide()
			if err != nil {
				return nil, err
			}
			return NewCatalog([]map[string]string{{"id": "index", "revision": snapshot.Revision}}), nil
		},
		get: func(_ context.Context, request ReadRequest) (any, error) {
			if request.IDs[0] != "index" {
				return nil, Missing(fmt.Errorf("unknown lore_index ID %q; use index", request.IDs[0]))
			}
			snapshot, err := store.IndexGuide()
			return loreIndexDocument{ID: "index", IndexGuideSnapshot: snapshot}, err
		},
		apply: func(_ context.Context, mutation Mutation) (any, error) {
			if mutation.ID != "index" {
				return nil, fmt.Errorf("unknown lore_index ID %q; use index", mutation.ID)
			}
			var guide lore.IndexGuide
			if err := decodeConfigValue(mutation.Value, &guide); err != nil {
				return nil, err
			}
			// Require the complete document to prevent a partial group edit from
			// silently removing every other group and its item associations.
			if guide.Groups == nil {
				return nil, fmt.Errorf("lore_index requires the complete guide with groups; use [] to remove all custom groups")
			}
			snapshot, err := store.UpdateIndexGuide(lore.IndexGuideUpdate{Guide: guide, BaseRevision: mutation.Revision})
			if errors.Is(err, lore.ErrRevisionConflict) {
				return nil, fmt.Errorf("lore_index revision conflict; read the current guide again")
			}
			if err != nil {
				return nil, err
			}
			return configMutationReceipt{Resource: mutation.Resource, Operation: mutation.Operation, ID: "index", Revision: snapshot.Revision}, nil
		},
	}
}

// This projection intentionally excludes bodies and assets. Use Lore read
// tools when content is needed; configuration reads also include disabled items.
type loreIndexMembershipDocument struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Enabled          bool                   `json:"enabled"`
	Type             string                 `json:"type"`
	LoadMode         string                 `json:"load_mode"`
	IndexMemberships []lore.IndexMembership `json:"index_memberships"`
	Revision         string                 `json:"revision"`
}

func loreIndexMembershipValue(item lore.Item) loreIndexMembershipDocument {
	memberships := item.IndexMemberships
	if memberships == nil {
		memberships = []lore.IndexMembership{}
	}
	return loreIndexMembershipDocument{
		ID: item.ID, Name: item.Name, Enabled: item.Enabled, Type: item.Type, LoadMode: item.LoadMode,
		IndexMemberships: memberships, Revision: item.UpdatedAt,
	}
}

func newLoreIndexMembershipResource(workspace string) Adapter {
	store := lore.NewStore(workspace)
	return configResourceAdapter{
		descriptor: Descriptor{
			Name: "lore_index_membership", Description: "Custom index-group associations and detail overrides for each existing lore item, including disabled items. List query matches ID or name. Update replaces only index_memberships; [] restores automatic grouping. Apply one item at a time and retry only failures.",
			Scopes: []string{"workspace"}, Operations: []string{ReadList, ReadGet, ApplyUpdate}, RevisionField: "revision", Reference: "references/lore-index.md",
		},
		list: func(_ context.Context, request ReadRequest) (any, error) {
			items, err := store.ListAll()
			if err != nil {
				return nil, err
			}
			values := []loreIndexMembershipDocument{}
			query := strings.ToLower(request.Query)
			for _, item := range items {
				if query == "" || strings.Contains(strings.ToLower(item.ID), query) || strings.Contains(strings.ToLower(item.Name), query) {
					values = append(values, loreIndexMembershipValue(item))
				}
			}
			return NewCatalog(values), nil
		},
		get: func(_ context.Context, request ReadRequest) (any, error) {
			item, err := store.Get(request.IDs[0])
			return loreIndexMembershipValue(item), err
		},
		apply: func(ctx context.Context, mutation Mutation) (any, error) {
			var value struct {
				IndexMemberships []lore.IndexMembership `json:"index_memberships"`
			}
			if err := decodeConfigValue(mutation.Value, &value); err != nil {
				return nil, err
			}
			if value.IndexMemberships == nil {
				return nil, fmt.Errorf("index_memberships must be an array; use [] to remove all custom-group associations")
			}
			// Resolve the exact ID before the patch API normalizes identifiers.
			if _, err := store.Get(mutation.ID); err != nil {
				return nil, err
			}
			result, err := store.ApplyOperations("Update lore index memberships", []lore.Operation{{
				Op: "update", ID: mutation.ID,
				Item: lore.ItemInput{IndexMemberships: value.IndexMemberships, BaseRevision: mutation.Revision},
			}})
			if errors.Is(err, lore.ErrRevisionConflict) {
				return nil, fmt.Errorf("lore_index_membership revision conflict; read the current item again")
			}
			if err != nil {
				return nil, err
			}
			item := result.Updated[0]
			slog.InfoContext(ctx, "[lore] index memberships updated", "item_id", item.ID, "groups", len(item.IndexMemberships))
			return configMutationReceipt{Resource: mutation.Resource, Operation: mutation.Operation, ID: item.ID, Revision: item.UpdatedAt}, nil
		},
	}
}

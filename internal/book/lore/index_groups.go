package lore

import (
	"fmt"
	"slices"
)

// IndexAutomaticGroup is an editor projection, never a model context fragment.
// Display preferences are saved in the guide; membership follows current items.
type IndexAutomaticGroup struct {
	Key           string   `json:"key"`
	CategoryID    string   `json:"category_id,omitempty"`
	LoadMode      string   `json:"load_mode"`
	DefaultDetail string   `json:"default_detail"`
	ItemIDs       []string `json:"item_ids"`
}

type IndexPreview struct {
	Markdown        string                `json:"markdown"`
	AutomaticGroups []IndexAutomaticGroup `json:"automatic_groups"`
	CustomItemIDs   map[string][]string   `json:"custom_item_ids"`
	OverBudget      bool                  `json:"over_budget"`
}

type resolvedIndexGroup struct {
	IndexGroup
	items     []Item
	automatic *IndexAutomaticGroup
}

func automaticGroupKey(mode, category string) string {
	if mode == LoadModeResident {
		return mode
	}
	return mode + ":" + category
}

func (guide IndexGuide) automaticDetail(mode, category string) string {
	if detail := guide.AutomaticDetails[automaticGroupKey(mode, category)]; detail != "" {
		return detail
	}
	if mode == LoadModeResident {
		return IndexDetailFull
	}
	return IndexDetailName
}

// resolveIndexGroups is the shared source for preview, injection and group
// queries. Authored membership wins; unassigned entries need no persisted links.
func resolveIndexGroups(collection Collection) []resolvedIndexGroup {
	guide := collection.IndexGuide
	items := []Item{}
	for _, item := range collection.Items {
		items = append(items, resolveItem(item, collection.Assets))
	}
	groups := []resolvedIndexGroup{}
	assigned := map[string]bool{}
	for _, group := range guide.Groups {
		resolved := resolvedIndexGroup{IndexGroup: group}
		for _, item := range items {
			if membershipDetail(item, group) != "" {
				resolved.items = append(resolved.items, item)
				assigned[item.ID] = true
			}
		}
		orderIndexItems(resolved.items, guide.ItemOrder["custom:"+group.ID])
		groups = append(groups, resolved)
	}
	appendAutomatic := func(mode, categoryID, name string) {
		key := automaticGroupKey(mode, categoryID)
		metadata := &IndexAutomaticGroup{Key: key, CategoryID: categoryID, LoadMode: mode, DefaultDetail: guide.automaticDetail(mode, categoryID), ItemIDs: []string{}}
		group := resolvedIndexGroup{IndexGroup: IndexGroup{Name: name, DefaultDetail: metadata.DefaultDetail}, automatic: metadata}
		for _, item := range items {
			if !item.Enabled || assigned[item.ID] || item.LoadMode != mode || (mode != LoadModeResident && item.Type != categoryID) {
				continue
			}
			group.items = append(group.items, item)
		}
		if len(group.items) == 0 {
			return
		}
		orderIndexItems(group.items, guide.ItemOrder["automatic:"+key])
		for _, item := range group.items {
			metadata.ItemIDs = append(metadata.ItemIDs, item.ID)
		}
		// Authored titles remain freeform; automatic query names are disambiguated.
		for suffix := 2; slices.ContainsFunc(groups, func(g resolvedIndexGroup) bool { return loreNameKey(g.Name) == loreNameKey(group.Name) }); suffix++ {
			group.Name = fmt.Sprintf("%s (%d)", name, suffix)
		}
		groups = append(groups, group)
	}
	appendAutomatic(LoadModeResident, "", "Resident")
	for _, category := range collection.Categories {
		appendAutomatic(LoadModeAuto, category.ID, category.DisplayName()+" · On demand")
		appendAutomatic(LoadModeManual, category.ID, category.DisplayName()+" · Manual")
	}
	applyIndexOrder(groups, guide.GroupOrder, func(group resolvedIndexGroup) string {
		if group.automatic != nil {
			return "automatic:" + group.automatic.Key
		}
		return "custom:" + group.ID
	})
	return groups
}

func selectIndexGroups(groups []resolvedIndexGroup, names []string) ([]resolvedIndexGroup, error) {
	selected := []resolvedIndexGroup{}
	seen := map[string]bool{}
	for _, name := range names {
		key := loreNameKey(name)
		if seen[key] {
			continue
		}
		index := slices.IndexFunc(groups, func(g resolvedIndexGroup) bool { return loreNameKey(g.Name) == key })
		if index < 0 {
			return nil, fmt.Errorf("unknown lore group %q; use the exact heading from the Lore Index", name)
		}
		seen[key] = true
		selected = append(selected, groups[index])
	}
	return selected, nil
}

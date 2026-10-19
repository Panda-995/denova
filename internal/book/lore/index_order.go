package lore

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Missing preferences retain the default order. Hidden automatic groups and
// disabled/unlinked items keep their positions if they become visible again.
func applyIndexOrder[T any](values []T, order []string, key func(T) string) {
	ranks := make(map[string]int, len(order))
	for i, id := range order {
		ranks[id] = i
	}
	rank := func(value T) int {
		if position, ok := ranks[key(value)]; ok {
			return position
		}
		return len(order)
	}
	sort.SliceStable(values, func(i, j int) bool { return rank(values[i]) < rank(values[j]) })
}

func orderIndexItems(items []Item, order []string) {
	sort.Slice(items, func(i, j int) bool { return loreNameKey(items[i].Name) < loreNameKey(items[j].Name) })
	applyIndexOrder(items, order, func(item Item) string { return item.ID })
}

func validateIndexOrder(guide IndexGuide) error {
	validKey := func(key string) bool {
		kind, id, _ := strings.Cut(key, ":")
		if kind == "automatic" && id == LoadModeResident {
			return true
		}
		if kind == "automatic" {
			mode, category, _ := strings.Cut(id, ":")
			if mode != LoadModeAuto && mode != LoadModeManual {
				return false
			}
			id = category
		} else if kind != "custom" {
			return false
		}
		return id != "" && len(id) <= 64 && normalizeLoreID(id) == id
	}
	if len(guide.GroupOrder) > 769 || len(guide.ItemOrder) > 769 {
		return fmt.Errorf("%w: too many ordered groups", ErrIndexGuide)
	}
	seen := map[string]bool{}
	for _, key := range guide.GroupOrder {
		if !validKey(key) || seen[key] {
			return fmt.Errorf("%w: invalid or duplicate group order key %q", ErrIndexGuide, key)
		}
		seen[key] = true
	}
	for key, order := range guide.ItemOrder {
		if !validKey(key) {
			return fmt.Errorf("%w: invalid item order group %q", ErrIndexGuide, key)
		}
		seen := map[string]bool{}
		for _, id := range order {
			if id == "" || normalizeLoreID(id) != id || seen[id] {
				return fmt.Errorf("%w: invalid or duplicate ordered item %q", ErrIndexGuide, id)
			}
			seen[id] = true
		}
	}
	return nil
}

// Delete only references to removed identities; an empty group is not deleted.
func pruneIndexOrder(collection *Collection) {
	keys := map[string]bool{"automatic:resident": true}
	for _, group := range collection.IndexGuide.Groups {
		keys["custom:"+group.ID] = true
	}
	for _, category := range collection.Categories {
		keys["automatic:auto:"+category.ID] = true
		keys["automatic:manual:"+category.ID] = true
	}
	items := map[string]bool{}
	for _, item := range collection.Items {
		items[item.ID] = true
	}
	guide := &collection.IndexGuide
	guide.GroupOrder = slices.DeleteFunc(guide.GroupOrder, func(key string) bool { return !keys[key] })
	for key, order := range guide.ItemOrder {
		if !keys[key] {
			delete(guide.ItemOrder, key)
		} else {
			guide.ItemOrder[key] = slices.DeleteFunc(order, func(id string) bool { return !items[id] })
		}
	}
}

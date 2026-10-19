package lore

import (
	"container/list"
	"maps"
	"slices"
	"sync"

	"denova/internal/revisionfile"
)

// Cache only decoding, keyed by the exact content revision read under the
// existing file lock. External edits, restores and same-size replacements are
// therefore visible immediately; this is never a second persistence source.
var decodedCollections = struct {
	sync.Mutex
	entries list.List
	bytes   int
}{}

type decodedCollection struct {
	revision string
	value    Collection
	bytes    int
}

func decodeCollectionSnapshot(snapshot revisionfile.Snapshot) (Collection, error) {
	decodedCollections.Lock()
	defer decodedCollections.Unlock()
	for entry := decodedCollections.entries.Front(); entry != nil; entry = entry.Next() {
		cached := entry.Value.(decodedCollection)
		if cached.revision == snapshot.Revision {
			decodedCollections.entries.MoveToFront(entry)
			return cloneCollection(cached.value), nil
		}
	}
	collection, err := DecodeCollection(snapshot.Content)
	if err != nil {
		return Collection{}, err
	}
	const maxBytes = 32 << 20
	if len(snapshot.Content) <= maxBytes {
		for decodedCollections.entries.Len() >= 8 || decodedCollections.bytes+len(snapshot.Content) > maxBytes {
			last := decodedCollections.entries.Back()
			decodedCollections.bytes -= last.Value.(decodedCollection).bytes
			decodedCollections.entries.Remove(last)
		}
		decodedCollections.entries.PushFront(decodedCollection{snapshot.Revision, cloneCollection(collection), len(snapshot.Content)})
		decodedCollections.bytes += len(snapshot.Content)
	}
	return collection, nil
}

// Callers mutate collections and return item slices to other domains. Clone
// every mutable member while sharing immutable strings (including large bodies).
func cloneCollection(value Collection) Collection {
	value.Categories = slices.Clone(value.Categories)
	value.Assets = slices.Clone(value.Assets)
	value.Items = slices.Clone(value.Items)
	for i := range value.Items {
		item := &value.Items[i]
		item.Tags = slices.Clone(item.Tags)
		item.Keywords = slices.Clone(item.Keywords)
		item.IndexMemberships = slices.Clone(item.IndexMemberships)
		item.ResolvedMaterials = slices.Clone(item.ResolvedMaterials)
		if item.Image != nil {
			image := *item.Image
			item.Image = &image
		}
		if item.Provenance != nil {
			provenance := *item.Provenance
			item.Provenance = &provenance
		}
		if item.Materials != nil {
			materials := *item.Materials
			materials.Entries = slices.Clone(materials.Entries)
			item.Materials = &materials
		}
	}
	value.IndexGuide.Groups = slices.Clone(value.IndexGuide.Groups)
	value.IndexGuide.GroupOrder = slices.Clone(value.IndexGuide.GroupOrder)
	value.IndexGuide.AutomaticDetails = maps.Clone(value.IndexGuide.AutomaticDetails)
	value.IndexGuide.ItemOrder = maps.Clone(value.IndexGuide.ItemOrder)
	for key, items := range value.IndexGuide.ItemOrder {
		value.IndexGuide.ItemOrder[key] = slices.Clone(items)
	}
	return value
}

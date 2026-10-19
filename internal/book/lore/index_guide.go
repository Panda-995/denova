package lore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"denova/internal/revisionfile"
)

const (
	IndexDetailInherit = "inherit"
	IndexDetailName    = "name"
	IndexDetailBrief   = "brief"
	IndexDetailFull    = "full"
	// IndexContextMaxBytes includes complete always-loaded bodies. Overflow is
	// an actionable error, never silent truncation of canon or guide prose.
	IndexContextMaxBytes = ResidentLoreSafetyMaxBytes + IndexDefaultMaxBytes
)

var ErrIndexGuide = errors.New("invalid lore index guide")
var ErrIndexContextTooLarge = errors.New("lore index context too large")

// IndexGuide is an authored document with ordered sections. Membership belongs
// exclusively to Item; both editors project and mutate the same relationship.
type IndexGuide struct {
	IntroMarkdown    string            `json:"intro_markdown"`
	Groups           []IndexGroup      `json:"groups"`
	AutomaticDetails map[string]string `json:"automatic_details,omitempty"`
	// Order contains presentation preferences, never a second membership list.
	// Keys are custom:<group ID> or automatic:<mode[:category ID]>.
	GroupOrder []string            `json:"group_order,omitempty"`
	ItemOrder  map[string][]string `json:"item_order,omitempty"`
}

type IndexGroup struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Purpose       string `json:"purpose"`
	BodyMarkdown  string `json:"body_markdown"`
	DefaultDetail string `json:"default_detail"`
}

type IndexMembership struct {
	GroupID string `json:"group_id"`
	Detail  string `json:"detail"`
}

type IndexGuideSnapshot struct {
	Guide    IndexGuide `json:"guide"`
	Revision string     `json:"revision"`
}

type IndexGuideUpdate struct {
	Guide        IndexGuide `json:"guide"`
	BaseRevision string     `json:"base_revision"`
}

func (s *Store) IndexGuide() (IndexGuideSnapshot, error) {
	return s.indexGuideSnapshot()
}

func (s *Store) indexGuideSnapshot() (IndexGuideSnapshot, error) {
	path, _ := s.readableItemsPath()
	snapshot, err := revisionfile.Read(context.Background(), path)
	if err != nil {
		return IndexGuideSnapshot{}, err
	}
	var collection Collection
	if snapshot.Exists {
		collection, err = DecodeCollection(snapshot.Content)
		if err != nil {
			return IndexGuideSnapshot{}, err
		}
	}
	if collection.IndexGuide.Groups == nil {
		collection.IndexGuide.Groups = []IndexGroup{}
	}
	return IndexGuideSnapshot{Guide: collection.IndexGuide, Revision: snapshot.Revision}, nil
}

// UpdateIndexGuide is a compare-and-swap on the canonical collection. Removing
// a section clears its memberships atomically and keeps every item and body.
func (s *Store) UpdateIndexGuide(input IndexGuideUpdate) (IndexGuideSnapshot, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	current, err := s.indexGuideSnapshot()
	if err != nil {
		return IndexGuideSnapshot{}, err
	}
	if input.BaseRevision == "" || input.BaseRevision != current.Revision {
		return IndexGuideSnapshot{}, ErrRevisionConflict
	}
	collection, err := s.loadOrCreate()
	if err != nil {
		return IndexGuideSnapshot{}, err
	}
	collection.IndexGuide = input.Guide
	groups := map[string]bool{}
	for i := range collection.IndexGuide.Groups {
		group := &collection.IndexGuide.Groups[i]
		group.Name = strings.TrimSpace(group.Name)
		group.Purpose = strings.TrimSpace(group.Purpose)
		groups[group.ID] = true
	}
	removed := false
	for _, group := range current.Guide.Groups {
		removed = removed || !groups[group.ID]
	}
	for i := range collection.Items {
		item := &collection.Items[i]
		next := slices.DeleteFunc(slices.Clone(item.IndexMemberships), func(m IndexMembership) bool { return !groups[m.GroupID] })
		if len(next) != len(item.IndexMemberships) {
			item.IndexMemberships = next
			item.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	if err := validateIndexGuide(collection); err != nil {
		return IndexGuideSnapshot{}, err
	}
	if removed {
		snapshot, err := revisionfile.Read(context.Background(), s.itemsPath())
		if err != nil {
			return IndexGuideSnapshot{}, err
		}
		if err := s.backupCategorySnapshot("index-removal", snapshot); err != nil {
			return IndexGuideSnapshot{}, err
		}
	}
	if err := s.save(collection); err != nil {
		return IndexGuideSnapshot{}, err
	}
	slog.Info("[lore] index guide updated", "groups", len(collection.IndexGuide.Groups), "removed_groups", removed)
	return s.indexGuideSnapshot()
}

func validateIndexGuide(collection Collection) error {
	guide := collection.IndexGuide
	if err := validateIndexOrder(guide); err != nil {
		return err
	}
	if len(guide.Groups) > 256 || len(guide.IntroMarkdown) > IndexDefaultMaxBytes {
		return fmt.Errorf("%w: maximum 256 sections and 64 KiB introduction", ErrIndexGuide)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, group := range guide.Groups {
		name := loreNameKey(group.Name)
		if group.ID == "" || normalizeLoreID(group.ID) != group.ID || len(group.ID) > 64 || ids[group.ID] || name == "" || names[name] || len(group.Name) > 256 || strings.ContainsAny(group.Name, "\r\n\t") || len(group.Purpose) > 1024 {
			return fmt.Errorf("%w: sections need unique IDs and names (256 bytes) and a short purpose (1024 bytes)", ErrIndexGuide)
		}
		if indexDetailRank(group.DefaultDetail) == 0 {
			return fmt.Errorf("%w: default detail must be name, brief or full", ErrIndexGuide)
		}
		ids[group.ID], names[name] = true, true
	}
	if len(guide.AutomaticDetails) > 513 {
		return fmt.Errorf("%w: too many automatic group presets", ErrIndexGuide)
	}
	for key, detail := range guide.AutomaticDetails {
		mode, category, _ := strings.Cut(key, ":")
		if indexDetailRank(detail) == 0 || (key != LoadModeResident && ((mode != LoadModeAuto && mode != LoadModeManual) || category == "")) {
			return fmt.Errorf("%w: invalid automatic group preset %q", ErrIndexGuide, key)
		}
	}
	for _, item := range collection.Items {
		seen := map[string]bool{}
		for _, membership := range item.IndexMemberships {
			if !ids[membership.GroupID] || seen[membership.GroupID] || (membership.Detail != IndexDetailInherit && indexDetailRank(membership.Detail) == 0) {
				return fmt.Errorf("%w: invalid membership on item %q", ErrIndexGuide, item.ID)
			}
			seen[membership.GroupID] = true
		}
	}
	return nil
}

func indexDetailRank(detail string) int {
	switch detail {
	case IndexDetailName:
		return 1
	case IndexDetailBrief:
		return 2
	case IndexDetailFull:
		return 3
	default:
		return 0
	}
}

func membershipDetail(item Item, group IndexGroup) string {
	for _, membership := range item.IndexMemberships {
		if membership.GroupID == group.ID {
			if membership.Detail == IndexDetailInherit {
				return group.DefaultDetail
			}
			return membership.Detail
		}
	}
	return ""
}

// IncludesFullBody uses the same effective detail as the initial index. Explicit
// memberships control grouped items; the legacy load mode applies only outside
// authored groups. This avoids injecting a second body from game turn references.
func (guide IndexGuide) IncludesFullBody(item Item) bool {
	return item.Enabled && guide.itemDetail(item) == IndexDetailFull
}

func (guide IndexGuide) itemDetail(item Item) string {
	detail := ""
	for _, group := range guide.Groups {
		candidate := membershipDetail(item, group)
		if indexDetailRank(candidate) > indexDetailRank(detail) {
			detail = candidate
		}
	}
	if detail == "" {
		detail = guide.automaticDetail(item.LoadMode, item.Type)
	}
	// Empty bodies expose a brief, never a claim that complete canon was read.
	if detail == IndexDetailFull && strings.TrimSpace(item.Content) == "" {
		return IndexDetailBrief
	}
	return detail
}

func (s *Store) indexQueryItems(options IndexOptions) ([]Item, []IndexGroup, error) {
	collection, err := s.loadOrCreate()
	if err != nil {
		return nil, nil, err
	}
	selected := map[string]bool{}
	var guides []IndexGroup
	if len(options.GroupNames) > 0 {
		groups, err := selectIndexGroups(resolveIndexGroups(collection), options.GroupNames)
		if err != nil {
			return nil, nil, err
		}
		for _, group := range groups {
			guides = append(guides, group.IndexGroup)
			for _, item := range group.items {
				selected[item.ID] = true
			}
		}
	}
	items := []Item{}
	for _, item := range collection.Items {
		if item.Enabled && (len(options.GroupNames) == 0 || selected[item.ID]) {
			items = append(items, resolveItem(item, collection.Assets))
		}
	}
	return items, guides, nil
}

// PreviewIndexGuide returns the exact model Markdown and ordered editor
// projections. Editor-only item IDs never appear in model Markdown. Oversized
// previews still return editable groups so the user can lower their detail.
func (s *Store) PreviewIndexGuide(guide IndexGuide) (IndexPreview, error) {
	collection, err := s.loadOrCreate()
	if err != nil {
		return IndexPreview{}, err
	}
	if err := validateIndexGuide(Collection{IndexGuide: guide}); err != nil {
		return IndexPreview{}, err
	}
	collection.IndexGuide = guide
	preview := IndexPreview{AutomaticGroups: []IndexAutomaticGroup{}, CustomItemIDs: map[string][]string{}}
	for _, group := range resolveIndexGroups(collection) {
		if group.automatic != nil {
			preview.AutomaticGroups = append(preview.AutomaticGroups, *group.automatic)
		} else {
			ids := []string{}
			for _, item := range group.items {
				ids = append(ids, item.ID)
			}
			preview.CustomItemIDs[group.ID] = ids
		}
	}
	preview.Markdown, err = renderIndexGuide(collection)
	if errors.Is(err, ErrIndexContextTooLarge) {
		preview.OverBudget = true
		return preview, nil
	}
	return preview, err
}

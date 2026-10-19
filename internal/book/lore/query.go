package lore

import "fmt"

// QueryOptions selects either exact identities or a filtered search. Exact
// identities cannot be combined with search filters; their order is retained.
type QueryOptions struct {
	IDs   []string
	Names []string
	IndexOptions
}

// QueryResult is one page of enabled items. Missing reports unresolved exact
// identities across the request, never items waiting on a subsequent page.
// Groups contains only the reading guides explicitly selected by the caller.
type QueryResult struct {
	Items      []Item
	Missing    []string
	Groups     []IndexGroup
	Total      int
	NextOffset *int
}

func (s *Store) Query(options QueryOptions) (QueryResult, error) {
	result := QueryResult{}
	exact := len(options.IDs) > 0 || len(options.Names) > 0
	if len(options.IDs) > 0 && len(options.Names) > 0 {
		return result, fmt.Errorf("provide either ids or names, not both")
	}
	if exact && (len(options.GroupNames) > 0 || len(options.Keywords) > 0 || len(options.Types) > 0 || len(options.LoadModes) > 0 || options.Match != "") {
		return result, fmt.Errorf("exact ids or names cannot be combined with search filters")
	}
	if options.Offset < 0 || options.Limit < 0 {
		return result, fmt.Errorf("offset and limit must not be negative")
	}
	if exact {
		var read ReadResult
		var err error
		if len(options.Names) > 0 {
			read, err = s.ReadManyNames(options.Names)
		} else {
			read, err = s.ReadMany(options.IDs)
		}
		if err != nil {
			return result, err
		}
		result.Missing = read.Missing
		result.Total = len(read.Items)
		start := min(options.Offset, result.Total)
		end := start + min(normalizeLoreIndexLimit(options.Limit), result.Total-start)
		result.Items = read.Items[start:end]
	} else {
		items, groups, err := s.indexQueryItems(options.IndexOptions)
		if err != nil {
			return result, err
		}
		options.Paginate = true
		entries, total, _ := filterLoreIndexEntries(items, options.IndexOptions)
		result.Total, result.Groups = total, groups
		for _, entry := range entries {
			result.Items = append(result.Items, entry.Item)
		}
	}
	if next := options.Offset + len(result.Items); next < result.Total {
		result.NextOffset = &next
	}
	return result, nil
}

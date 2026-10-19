package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"denova/config"
	"denova/internal/book/lore"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type queryLoreItemsInput struct {
	IDs        []string `json:"ids,omitempty" jsonschema_description:"Exact enabled lore item IDs. Use either ids or names, without search filters. Results preserve request order; missing identities are reported separately from pagination."`
	Names      []string `json:"names,omitempty" jsonschema_description:"Exact unique lore names. Prefer this when names are known from context. Do not combine with ids or search filters."`
	GroupNames []string `json:"group_names,omitempty" jsonschema_description:"Optional exact group names from the Lore Index. Multiple groups form a union; combine with keywords or categories to narrow results."`
	Keywords   []string `json:"keywords,omitempty" jsonschema_description:"Optional search terms. Each item independently matches ID, name, aliases, tags, description, and body. Do not combine several keywords into one string."`
	Match      string   `json:"match,omitempty" jsonschema:"enum=any,enum=all" jsonschema_description:"Relationship among keywords: any matches any keyword (OR, default); all requires every keyword (AND)."`
	Types      []string `json:"types,omitempty" jsonschema_description:"Optional exact category IDs from the project catalog returned by query_lore_items."`
	LoadModes  []string `json:"load_modes,omitempty" jsonschema_description:"Optional load modes: resident, auto, or manual. Prefer resident for state-schema review."`
	Detail     string   `json:"detail,omitempty" jsonschema:"enum=index,enum=full" jsonschema_description:"Result detail: index returns descriptions; full returns complete bodies. Omission defaults to index for all queries, including groups."`
	Limit      int      `json:"limit,omitempty" jsonschema_description:"Number of filtered results on this page, default 10. Unfiltered catalogs paginate automatically by index byte budget."`
	Offset     int      `json:"offset,omitempty" jsonschema_description:"Pagination offset, default 0. Continue from the returned next-page offset."`
}

type writeLoreItemsInput struct {
	Message   string               `json:"message,omitempty" jsonschema:"description=Optional change summary for this lore update; summarize briefly in Chinese."`
	Items     []writeLoreItemInput `json:"items,omitempty" jsonschema:"description=Lore items to create or partially update. Each array element is one independently retrievable entity or coherent topic; use separate elements for distinct characters, locations, factions, items, or world rules. Do not put the entire library or several unrelated entities into one element. Creation requires at least name. Updates require an existing id and only changed fields; omitted fields retain their values."`
	DeleteIDs []string             `json:"delete_ids,omitempty" jsonschema:"description=Lore item IDs to delete. Use only when the author explicitly requests deletion."`
}

type writeLoreItemInput struct {
	ID               string   `json:"id,omitempty" jsonschema:"description=Lore ID. An update requires the exact existing ID; creation may omit it for automatic generation."`
	Enabled          *bool    `json:"enabled,omitempty" jsonschema:"description=Whether the lore item is enabled. A disabled item remains stored but is excluded from the lore index, read tools, and model context. Omit when uncertain."`
	Type             string   `json:"type,omitempty" jsonschema:"description=Exact category ID from the project catalog. Creation defaults to world, or the first non-character category if world was removed; omission during update retains the current value."`
	Name             string   `json:"name,omitempty" jsonschema:"description=Lore name. Required for creation; omission during update retains the current value."`
	Importance       string   `json:"importance,omitempty" jsonschema:"description=Importance: major, important, or minor. Creation defaults to important; omission during update retains the current value."`
	Tags             []string `json:"tags,omitempty" jsonschema:"description=Tags. Omission during update retains the current value; an empty array clears it."`
	BriefDescription string   `json:"brief_description,omitempty" jsonschema:"description=Index description. Start with type and name, then use 3-5 sentences for identity, aliases, key facts, use cases, and trigger terms. Omission on creation generates it from the body; omission on update retains the current value."`
	Keywords         []string `json:"keywords,omitempty" jsonschema:"description=Aliases, keywords, or trigger terms. Omission during update retains the current value; an empty array clears it."`
	LoadMode         string   `json:"load_mode,omitempty" jsonschema:"description=Load mode: resident, auto, or manual. Creation infers it automatically; omission during update retains the current value."`
	Content          string   `json:"content,omitempty" jsonschema:"description=Markdown body for this entity or coherent topic only, in the author's language. Include its stable canon and relevant relationships; put other independently retrievable entities in separate items. Reference other entries with [[Exact Lore Name]] using their unique project-local names. References do not load target bodies automatically; read them by name when needed. Renaming an entry does not rewrite references, so update referring bodies when asked to rename consistently. On update this replaces the entire body: read the existing item first and preserve still-valid facts. Omission retains the current value. Put per-chapter current location, injuries, psychology, and goals in setting/character-states.md instead of lore."`
}

type loreToolsOptions struct {
	ReadPolicy *loreReadPolicy
}

// loreReadPolicy observes only lore bodies successfully returned to the model.
// Output sizing belongs to the shared tool-result projection boundary; Lore
// must not reject an otherwise valid batch using a second, stricter budget.
type loreReadPolicy struct {
	OnRead func([]string)
}

func (p *loreReadPolicy) observe(items []lore.Item) {
	if p == nil || p.OnRead == nil {
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		p.OnRead(ids)
	}
}

func newLoreTools(workspace string, allowWrite bool, options ...loreToolsOptions) ([]agenttool.ToolDefinition, error) {
	workspace = strings.TrimSpace(workspace)
	var readPolicy *loreReadPolicy
	if len(options) > 0 {
		readPolicy = options[0].ReadPolicy
	}
	queryTool, err := agenttool.InferTool("query_lore_items", "Query enabled lore by exact ids or unique names, or search by groups, keywords, categories and load modes. Exact selectors cannot be combined with search filters. detail=index (default) returns briefs; detail=full returns complete bodies. Both include material_count and cover_asset_id, not media content. Empty selectors return the project category and name catalog. Follow next_offset with the same arguments to continue; missing_ids or missing_names never include later-page results. Use list_lore_materials only when actual material metadata is needed. Re-read earlier lore observations here using their retained selectors; exact body reads require detail=full.", func(ctx context.Context, input queryLoreItemsInput) (string, error) {
		if workspace == "" {
			return "", fmt.Errorf("cannot query lore because the current workspace is unavailable")
		}
		if err := validateQueryLoreItemsInput(input); err != nil {
			return "", err
		}
		store := lore.NewStore(workspace)
		if !hasLoreQuerySelectors(input) {
			return store.NameCatalogMarkdown(lore.NameCatalogOptions{Offset: input.Offset, MaxBytes: lore.IndexDefaultMaxBytes})
		}
		if len(input.Types) > 0 {
			categories, err := store.Categories()
			if err != nil {
				return "", err
			}
			for _, id := range input.Types {
				if !lore.HasCategory(categories, id) {
					return "", fmt.Errorf("unknown category %q; call query_lore_items without selectors to read the catalog", id)
				}
			}
		}
		result, err := store.Query(lore.QueryOptions{
			IDs: input.IDs, Names: input.Names,
			IndexOptions: lore.IndexOptions{
				GroupNames: input.GroupNames, Keywords: input.Keywords, Match: input.Match,
				Types: input.Types, LoadModes: input.LoadModes, Limit: input.Limit, Offset: input.Offset,
			},
		})
		if err != nil {
			return "", err
		}
		output := formatLoreQueryResult(result, input)
		if strings.EqualFold(strings.TrimSpace(input.Detail), "full") {
			readPolicy.observe(result.Items)
		}
		return output, nil
	})
	if err != nil {
		return nil, err
	}
	definedQueryTool, err := defineTool(queryTool, boundedReadDescriptor(ToolSourceLore, config.AgentToolLoreRead, agentschema.ToolResultRecoveryRerun))
	if err != nil {
		return nil, err
	}
	materialTool, err := newLoreMaterialsTool(workspace)
	if err != nil {
		return nil, err
	}
	tools := []agenttool.ToolDefinition{definedQueryTool, materialTool}
	if !allowWrite {
		return tools, nil
	}
	writeTool, err := agenttool.InferTool("write_lore_items", "Batch-create, partially update, or delete lore items. Each item is one independently retrievable entity or coherent topic, such as a character, location, faction, item, or world rule. Organize library-wide updates into separate items in the same batch, not one omnibus entry. Keep related facts about the same entity together. Find matching existing items with query_lore_items and read their bodies before updating; reuse their exact IDs instead of creating duplicates. Creation requires at least name; updates send only changed fields, while omitted fields retain their values. A supplied content replaces the whole body, so preserve still-valid canon. The backend may generate brief_description on creation. Put post-chapter current location, injuries, psychology, goals, and possessions in setting/character-states.md instead of lore. Do not store chapter planning or future plot in lore.", func(ctx context.Context, input writeLoreItemsInput) (agentschema.ToolResult, error) {
		_ = ctx
		if workspace == "" {
			return agentschema.ToolResult{}, fmt.Errorf("cannot write lore because the current workspace is unavailable")
		}
		store := lore.NewStore(workspace)
		ops, err := buildWriteLoreOperations(store, input)
		if err != nil {
			return agentschema.ToolResult{}, err
		}
		result, err := store.ApplyOperations(input.Message, ops)
		if err != nil {
			return agentschema.ToolResult{}, err
		}
		details, err := json.Marshal(map[string]any{
			"schema": "lore.write.v1", "item_ids": writeLoreChangedItemIDs(result),
			"deleted_ids": result.DeletedIDs,
		})
		if err != nil {
			return agentschema.ToolResult{}, err
		}
		toolResult := agentschema.TextToolResult(formatWriteLoreItemsResult(result))
		toolResult.Details = details
		return toolResult, nil
	})
	if err != nil {
		return nil, err
	}
	definedWriteTool, err := defineTool(writeTool, workspaceWriteDescriptor(ToolSourceLore, config.AgentToolLoreWrite, agenttool.ToolRecoveryReconcilable))
	if err != nil {
		return nil, err
	}
	return append(tools, definedWriteTool), nil
}

func validateQueryLoreItemsInput(input queryLoreItemsInput) error {
	match := strings.TrimSpace(input.Match)
	if match != "" && match != lore.IndexMatchAny && match != lore.IndexMatchAll {
		return fmt.Errorf("match must be any or all")
	}
	validLoadModes := map[string]bool{lore.LoadModeResident: true, lore.LoadModeAuto: true, lore.LoadModeManual: true}
	for _, loadMode := range input.LoadModes {
		if !validLoadModes[strings.TrimSpace(loadMode)] {
			return fmt.Errorf("invalid lore load mode: %s", strings.TrimSpace(loadMode))
		}
	}
	if input.Limit < 0 {
		return fmt.Errorf("limit cannot be negative; omission defaults to %d", lore.IndexDefaultLimit)
	}
	if input.Offset < 0 {
		return fmt.Errorf("offset cannot be negative")
	}
	detail := strings.ToLower(strings.TrimSpace(input.Detail))
	if detail != "" && detail != "index" && detail != "full" {
		return fmt.Errorf("detail must be index or full")
	}
	if detail == "full" && !hasLoreQuerySelectors(input) {
		return fmt.Errorf("detail=full requires ids, names, group_names, keywords, types, or load_modes; an unbounded read of all lore bodies is not allowed")
	}
	return nil
}

func hasLoreQuerySelectors(input queryLoreItemsInput) bool {
	return len(input.IDs) > 0 || len(input.Names) > 0 || len(input.GroupNames) > 0 || len(input.Keywords) > 0 || len(input.Types) > 0 || len(input.LoadModes) > 0
}

// Query results keep identity, continuation and material summaries together.
// Only full results count as read canon; index results never imply a body read.
func formatLoreQueryResult(result lore.QueryResult, input queryLoreItemsInput) string {
	var sb strings.Builder
	next, _ := json.Marshal(result.NextOffset)
	fmt.Fprintf(&sb, "# Lore Items\n\ntotal: %d\noffset: %d\nreturned: %d\nnext_offset: %s\n\n", result.Total, input.Offset, len(result.Items), next)
	if len(result.Missing) > 0 {
		field := "missing_ids"
		if len(input.Names) > 0 {
			field = "missing_names"
		}
		missing, _ := json.Marshal(result.Missing)
		fmt.Fprintf(&sb, "%s: %s\n", field, missing)
	}
	fmt.Fprintln(&sb)
	for _, group := range result.Groups {
		fmt.Fprintf(&sb, "# Group: %s\n\n%s\n\n%s\n\n", group.Name, group.Purpose, group.BodyMarkdown)
	}
	full := strings.EqualFold(strings.TrimSpace(input.Detail), "full")
	for _, item := range result.Items {
		if !full {
			item.Content = ""
		}
		fmt.Fprintln(&sb, lore.ReferenceMarkdown(item))
		fmt.Fprintln(&sb, loreMaterialSummaryMarkdown(item))
		fmt.Fprintln(&sb)
	}
	return strings.TrimSpace(sb.String())
}

func buildWriteLoreOperations(store *lore.Store, input writeLoreItemsInput) ([]lore.Operation, error) {
	itemsByID := map[string]lore.Item{}
	// Explicit write IDs may refer to disabled entries. Those entries stay out
	// of model read tools, but an author-approved review snapshot can still
	// safely drive an update without accidentally treating the ID as a create.
	existing, err := store.ListAll()
	if err != nil {
		return nil, err
	}
	for _, item := range existing {
		itemsByID[item.ID] = item
	}
	ops := make([]lore.Operation, 0, len(input.Items)+len(input.DeleteIDs))
	for _, item := range input.Items {
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		loreInput := lore.ItemInput{
			ID:               item.ID,
			Enabled:          item.Enabled,
			Type:             item.Type,
			Name:             item.Name,
			Importance:       item.Importance,
			Tags:             item.Tags,
			BriefDescription: item.BriefDescription,
			Keywords:         item.Keywords,
			LoadMode:         item.LoadMode,
			Content:          item.Content,
		}
		op := "create"
		if item.ID != "" {
			if _, ok := itemsByID[item.ID]; ok {
				op = "update"
			}
		}
		if op == "create" && item.Name == "" {
			return nil, fmt.Errorf("name is required when creating lore")
		}
		if op == "update" && !hasWriteLoreItemChanges(item) {
			return nil, fmt.Errorf("updating lore item %s requires at least one changed field", item.ID)
		}
		ops = append(ops, lore.Operation{Op: op, ID: item.ID, Item: loreInput})
	}
	for _, id := range input.DeleteIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ops = append(ops, lore.Operation{Op: "delete", ID: id})
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("there are no lore changes to write")
	}
	return ops, nil
}

func hasWriteLoreItemChanges(item writeLoreItemInput) bool {
	return item.Enabled != nil || strings.TrimSpace(item.Type) != "" || strings.TrimSpace(item.Name) != "" ||
		strings.TrimSpace(item.Importance) != "" || item.Tags != nil || strings.TrimSpace(item.BriefDescription) != "" ||
		item.Keywords != nil || strings.TrimSpace(item.LoadMode) != "" || strings.TrimSpace(item.Content) != ""
}

func formatWriteLoreItemsResult(result lore.ApplyResult) string {
	changed := []string{}
	if len(result.Created) > 0 {
		changed = append(changed, fmt.Sprintf("created %d", len(result.Created)))
	}
	if len(result.Updated) > 0 {
		changed = append(changed, fmt.Sprintf("updated %d", len(result.Updated)))
	}
	if len(result.DeletedIDs) > 0 {
		changed = append(changed, fmt.Sprintf("deleted %d", len(result.DeletedIDs)))
	}
	message := strings.TrimSpace(result.Message)
	if message == "" {
		message = "Lore updated"
	}
	if len(changed) > 0 {
		message += "（" + strings.Join(changed, "，") + "）"
	}
	itemIDs := writeLoreChangedItemIDs(result)
	itemIDsJSON, _ := json.Marshal(itemIDs)
	deletedIDsJSON, _ := json.Marshal(result.DeletedIDs)
	lines := []string{message}
	lines = append(lines, "item_ids: "+string(itemIDsJSON))
	lines = append(lines, "deleted_ids: "+string(deletedIDsJSON))
	return strings.Join(lines, "\n")
}

func writeLoreChangedItemIDs(result lore.ApplyResult) []string {
	ids := make([]string, 0, len(result.Created)+len(result.Updated)+len(result.DeletedIDs))
	seen := map[string]bool{}
	for _, item := range result.Created {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			ids = append(ids, item.ID)
		}
	}
	for _, item := range result.Updated {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			ids = append(ids, item.ID)
		}
	}
	for _, id := range result.DeletedIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"denova/internal/book/lore"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestLoreQueryExactSelectionPaginationAndMaterialSummary(t *testing.T) {
	workspace := t.TempDir()
	store := lore.NewStore(workspace)
	disabled := false
	for _, input := range []lore.ItemInput{
		{ID: "hero", Name: "Hero", BriefDescription: "Pilot", Content: "HERO_BODY", Image: &lore.Image{ImagePath: "assets/lore/hero.png", MIMEType: "image/png"}},
		{ID: "harbor", Name: "Harbor", BriefDescription: "Port", Content: "Hero docks here."},
		{ID: "hidden", Name: "Hidden", Enabled: &disabled, Content: "HIDDEN_BODY"},
	} {
		if _, err := store.Create(input); err != nil {
			t.Fatal(err)
		}
	}
	observed := []string{}
	definitions, err := newLoreTools(workspace, false, loreToolsOptions{ReadPolicy: &loreReadPolicy{OnRead: func(ids []string) { observed = append(observed, ids...) }}})
	if err != nil {
		t.Fatal(err)
	}
	var query agenttool.Tool
	for _, definition := range definitions {
		info, err := definition.Tool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "query_lore_items" {
			query = definition.Tool
		}
	}
	if query == nil {
		t.Fatal("query tool missing")
	}
	run := func(input string) string {
		t.Helper()
		result, err := query.Run(context.Background(), input)
		if err != nil || result.Status != agentschema.ToolResultSuccess {
			t.Fatalf("query failed: %#v %v", result, err)
		}
		return result.ModelContent
	}
	index := run(`{"names":["Hero"]}`)
	for _, want := range []string{"Pilot", "material_count: 1", `cover_asset_id: "legacy:hero:assets/lore/hero.png"`, "next_offset: null"} {
		if !strings.Contains(index, want) {
			t.Fatalf("missing %q: %s", want, index)
		}
	}
	if strings.Contains(index, "HERO_BODY") || strings.Contains(index, "docks here") || len(observed) != 0 {
		t.Fatalf("index leaked or observed bodies: %s %v", index, observed)
	}
	first := run(`{"ids":["harbor","missing","hero","hidden","harbor"],"detail":"full","limit":1}`)
	if !strings.Contains(first, "Hero docks here.") || strings.Contains(first, "HERO_BODY") || !strings.Contains(first, `missing_ids: ["missing","hidden"]`) || !strings.Contains(first, "next_offset: 1") {
		t.Fatal(first)
	}
	second := run(`{"ids":["harbor","missing","hero","hidden","harbor"],"detail":"full","limit":1,"offset":1}`)
	if !strings.Contains(second, "HERO_BODY") || !strings.Contains(second, "next_offset: null") || !reflect.DeepEqual(observed, []string{"harbor", "hero"}) {
		t.Fatalf("incorrect exact continuation: %s %v", second, observed)
	}
	missing := run(`{"names":["Absent","Hidden"]}`)
	if !strings.Contains(missing, `missing_names: ["Absent","Hidden"]`) || !strings.Contains(missing, "total: 0") {
		t.Fatal(missing)
	}
	search := run(`{"keywords":["Hero"],"detail":"full","limit":1}`)
	if !strings.Contains(search, "total: 2") || !strings.Contains(search, "next_offset: 1") {
		t.Fatal(search)
	}
	for _, input := range []string{
		`{"ids":["hero"],"names":["Hero"]}`, `{"ids":["hero"],"keywords":["Hero"]}`,
		`{"names":["Hero"],"group_names":["Cast"]}`, `{"names":["Hero"],"types":["character"]}`,
		`{"ids":["hero"],"load_modes":["auto"]}`, `{"ids":["hero"],"match":"any"}`,
		`{"ids":["hero"],"offset":-1}`, `{"detail":"full"}`,
	} {
		result, err := query.Run(context.Background(), input)
		if err == nil && result.Status == agentschema.ToolResultSuccess {
			t.Fatalf("accepted invalid query: %s", input)
		}
	}
}

func TestLoreGroupQueriesPreserveMarkdownAndObserveOnlyFullBodies(t *testing.T) {
	workspace := t.TempDir()
	store := lore.NewStore(workspace)
	current, err := store.IndexGuide()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateIndexGuide(lore.IndexGuideUpdate{BaseRevision: current.Revision, Guide: lore.IndexGuide{Groups: []lore.IndexGroup{
		{ID: "harbor", Name: "Harbor cast", Purpose: "Reusable people", BodyMarkdown: "GROUP_GUIDE", DefaultDetail: lore.IndexDetailFull},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []lore.ItemInput{
		{Name: "Captain", BriefDescription: "Ship captain", Content: "The captain carries an amber compass.", IndexMemberships: []lore.IndexMembership{{GroupID: "harbor", Detail: lore.IndexDetailInherit}}},
		{Name: "Doctor", BriefDescription: "Ship doctor", Content: "The doctor guards a blue crystal.", IndexMemberships: []lore.IndexMembership{{GroupID: "harbor", Detail: lore.IndexDetailName}}},
		{Name: "Unsorted", Content: "AMBER_BODY_ONLY_NEEDLE", BriefDescription: "A place", LoadMode: lore.LoadModeManual},
	} {
		if _, err := store.Create(input); err != nil {
			t.Fatal(err)
		}
	}
	observed := []string{}
	definitions, err := newLoreTools(workspace, false, loreToolsOptions{ReadPolicy: &loreReadPolicy{OnRead: func(ids []string) { observed = append(observed, ids...) }}})
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		info, err := definition.Tool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "query_lore_items" {
			continue
		}
		run := func(input string) string {
			t.Helper()
			result, err := definition.Tool.Run(context.Background(), input)
			if err != nil || result.Status != agentschema.ToolResultSuccess {
				t.Fatalf("list failed: %#v %v", result, err)
			}
			return result.ModelContent
		}
		group := run(`{"group_names":["Harbor cast"]}`)
		if !strings.Contains(group, "# Lore Items") || !strings.Contains(group, "GROUP_GUIDE") || strings.Contains(group, "amber compass") || strings.Contains(group, "blue crystal") || !strings.Contains(group, "Ship doctor") || len(observed) != 0 {
			t.Fatalf("incorrect group output or read receipt: %s %v", group, observed)
		}
		full := run(`{"group_names":["Harbor cast"],"detail":"full"}`)
		if !strings.Contains(full, "amber compass") || !strings.Contains(full, "blue crystal") || !reflect.DeepEqual(observed, []string{"captain", "doctor"}) {
			t.Fatalf("full group read: %s %v", full, observed)
		}
		observed = nil
		index := run(`{"group_names":["Harbor cast"],"detail":"index","limit":1}`)
		if strings.Contains(index, "amber compass") || len(observed) != 0 || !strings.Contains(index, "next_offset: 1") {
			t.Fatalf("index page: %s %v", index, observed)
		}
		search := run(`{"keywords":["AMBER_BODY_ONLY_NEEDLE"],"detail":"full"}`)
		if !strings.Contains(search, "AMBER_BODY_ONLY_NEEDLE") || !reflect.DeepEqual(observed, []string{"unsorted"}) {
			t.Fatalf("whole-library search excluded ungrouped/manual content: %s", search)
		}
		return
	}
	t.Fatal("query_lore_items was not registered")
}

func TestWriteLoreItemsKeepsBatchEntitiesSeparateDuringPartialUpdates(t *testing.T) {
	workspace := t.TempDir()
	definitions, err := newLoreTools(workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		info, err := definition.Tool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "write_lore_items" {
			continue
		}
		run := func(input string, wantIDs []string) {
			t.Helper()
			result, err := definition.Tool.Run(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != agentschema.ToolResultSuccess {
				t.Fatalf("write failed: %s", result.ModelContent)
			}
			var receipt struct {
				ItemIDs []string `json:"item_ids"`
			}
			if err := json.Unmarshal(result.Details, &receipt); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(receipt.ItemIDs, wantIDs) {
				t.Fatalf("receipt item IDs = %v, want %v", receipt.ItemIDs, wantIDs)
			}
		}
		run(`{"items":[
			{"id":"hero","type":"character","name":"Mira","tags":["pilot"],"content":"Mira pilots the ferry."},
			{"id":"harbor","type":"location","name":"North Harbor","content":"The harbor closes at night."},
			{"id":"guild","type":"faction","name":"River Guild","content":"The guild maintains the ferry."}
		]}`, []string{"hero", "harbor", "guild"})
		store := lore.NewStore(workspace)
		before, err := store.ListAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(before) != 3 {
			t.Fatalf("created %d items, want 3", len(before))
		}
		run(`{"items":[
			{"id":"hero","content":"Mira pilots the ferry and founded the guild."},
			{"id":"guild","content":"The guild maintains the ferry and was founded by Mira."}
		]}`, []string{"hero", "guild"})
		after, err := lore.NewStore(workspace).ListAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("updated collection has %d items, want %d", len(after), len(before))
		}
		want := append([]lore.Item(nil), before...)
		for i := range want {
			switch want[i].ID {
			case "hero":
				want[i].Content = "Mira pilots the ferry and founded the guild."
				want[i].UpdatedAt = after[i].UpdatedAt
			case "guild":
				want[i].Content = "The guild maintains the ferry and was founded by Mira."
				want[i].UpdatedAt = after[i].UpdatedAt
			}
		}
		if !reflect.DeepEqual(after, want) {
			t.Fatalf("batch update changed unrelated data:\n got %#v\nwant %#v", after, want)
		}
		return
	}
	t.Fatal("write_lore_items was not registered")
}

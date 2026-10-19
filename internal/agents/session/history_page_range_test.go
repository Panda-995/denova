package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Exercise the product journal adapter, including cold reopen and every page.
// Bounding response rows alone does not bound the work needed to read them.
func TestHistoryPagesBoundLongToolTurnReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long-tools.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	encode := func(value any) {
		t.Helper()
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	encode(map[string]string{"role": "user", "content": "Read all chapters"})
	const count = 1_024
	body := strings.Repeat("x", 2_048)
	for i := 0; i < count; i++ {
		id := fmt.Sprint(i)
		encode(displayRecord{Type: historyTypeDisplay, RecordID: id, DisplayEvent: DisplayEvent{
			ID: id, Role: "tool_call", Name: "read", Args: `{}`, Result: body, Status: "success", RunID: "one-run",
		}})
	}
	result := "late result"
	encode(displayPatchRecord{Type: historyTypeDisplayPatch, TargetRecordID: "0", Result: &result, ArgsAppend: " complete"})
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("long-tools"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.Get("long-tools")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for before := -1; ; {
		reads := sess.JournalReplayStats().RangeBytesRead
		page, err := sess.ReadHistoryPage(t.Context(), before, 100)
		if err != nil {
			t.Fatal(err)
		}
		if read := sess.JournalReplayStats().RangeBytesRead - reads; read >= info.Size()/2 {
			t.Fatalf("one page scanned too much history: read=%d journal=%d", read, info.Size())
		}
		for _, entry := range page.Entries {
			if entry.Role == "user" {
				continue
			}
			want := body
			if entry.ID == "0" {
				want = result
				if entry.Args != "{} complete" {
					t.Fatalf("late argument patch changed: %q", entry.Args)
				}
			}
			if seen[entry.ID] || entry.Result != want {
				t.Fatalf("duplicate or incomplete tool %q", entry.ID)
			}
			seen[entry.ID] = true
		}
		if !page.HasMore {
			break
		}
		if before >= 0 && page.NextBefore >= before {
			t.Fatal("pagination did not advance")
		}
		before = page.NextBefore
	}
	if len(seen) != count {
		t.Fatalf("paged tools=%d want=%d", len(seen), count)
	}
}

func TestHistoryPagesKeepSegmentedAssistantCopiesHiddenAcrossTurnAnchors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("segmented-long-turn")
	if err != nil {
		t.Fatal(err)
	}
	var revision uint64
	appendSegmentedHistoryRun(t, sess, "long-run", 700, &revision)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err = store.Get("segmented-long-turn")
	if err != nil {
		t.Fatal(err)
	}
	want := sess.History()
	if len(want) != 701 {
		t.Fatalf("cold fixture is incomplete: rows=%d", len(want))
	}
	var got []HistoryEntry
	for before := -1; ; {
		page, err := sess.ReadHistoryPage(t.Context(), before, 37)
		if err != nil {
			t.Fatal(err)
		}
		got = append(page.Entries, got...)
		if !page.HasMore {
			break
		}
		before = page.NextBefore
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segmented reply changed across page anchors: got=%d want=%d", len(got), len(want))
	}
	// The index contains positions and classification, never a second transcript.
	index, err := os.ReadFile(filepath.Join(dir, "segmented-long-turn.idx.json"))
	if err != nil || strings.Contains(string(index), "answer-00-long-run") {
		t.Fatalf("history index retained assistant prose: %v", err)
	}
	// A later large message must not be read merely to discover the end of an
	// earlier page, including anchors inside one physical batched transaction.
	if err := sess.Append(agentschema.UserMessage(strings.Repeat("later ", 256<<10))); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "segmented-long-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	beforeReads := sess.JournalReplayStats().RangeBytesRead
	if _, err := sess.ReadHistoryPage(t.Context(), 500, 37); err != nil {
		t.Fatal(err)
	}
	if read := sess.JournalReplayStats().RangeBytesRead - beforeReads; read >= info.Size()/2 {
		t.Fatalf("older batched page read a later large message: read=%d journal=%d", read, info.Size())
	}
}

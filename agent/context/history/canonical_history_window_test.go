package history

import (
	"fmt"
	"reflect"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestArchivedContextPreservesRawCoordinatesAndState(t *testing.T) {
	raw, state, err := advanceContextState(nil, []agentschema.ContextFragment{testContextStateFragment("v1", "accepted state")}, ContextStateSnapshot{}, CompactionRecord{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		raw = append(raw, agentschema.UserMessage(fmt.Sprint("instruction ", i)), agentschema.AssistantMessage(fmt.Sprint("evidence ", i), nil))
	}
	active := 3
	compact := CompactionRecord{Version: 2, ID: "first", Revision: 1, Summary: "summary", ReplacementTo: 7, RetainedUserFrom: &active}
	window, archive := ArchiveHistory(raw, nil, compact, state)
	assertProjection := func() {
		t.Helper()
		if archive.Count(window) != len(raw) {
			t.Fatal("raw message count changed")
		}
		if err := ValidateContextStateSnapshotInArchive(state, window, archive); err != nil {
			t.Fatal(err)
		}
		expected, err := EffectiveCompactionMessages(raw, compact, true, 1024)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := archive.EffectiveCompactionMessages(window, compact, true, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatal("compaction window changed model messages")
		}
		fullGroups, fullEnds, fullBytes := compactionGroups(raw, raw, compact, true)
		groups, ends, bytes := archive.CompactionGroups(window, window, compact, true)
		if !reflect.DeepEqual(groups, fullGroups) || !reflect.DeepEqual(ends, fullEnds) || bytes != fullBytes {
			t.Fatal("compaction group coordinates changed")
		}
		for index := range raw {
			if archive.CompactionMessageIndex(window, compact, true, index) != compactionMessageIndex(raw, compact, true, index) {
				t.Fatalf("raw coordinate %d changed", index)
			}
		}
	}
	assertProjection()
	compact.ReplacementTo = 11
	window, archive = ArchiveHistory(window, archive, compact, state)
	assertProjection()
	expected, nextState, err := advanceContextState(raw, []agentschema.ContextFragment{testContextStateFragment("v1", "accepted state")}, state, compact, true)
	if err != nil {
		t.Fatal(err)
	}
	actual, windowState, err := archive.AdvanceContextState(window, []agentschema.ContextFragment{testContextStateFragment("v1", "accepted state")}, state, compact, true)
	if err != nil || !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(windowState, nextState) {
		t.Fatalf("state reinjection changed raw positions: %v", err)
	}
}

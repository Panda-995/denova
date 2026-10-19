package conversationjournal

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestExactLocationsReadOnlySelectedTransactionsAndVerifyContent(t *testing.T) {
	journal, path := largeJournal(t, 8, 512)
	defer journal.Close()
	want, err := journal.ReadRange(t.Context(), Range{After: 1})
	if err != nil {
		t.Fatal(err)
	}
	selected := []Location{want[0].Location, want[7].Location}
	before := journal.ReplayStats().RangeBytesRead
	got, err := journal.ReadLocations(t.Context(), selected)
	if err != nil || !reflect.DeepEqual(got, []Record{want[0], want[7]}) {
		t.Fatalf("exact records changed: got=%#v error=%v", got, err)
	}
	if read := journal.ReplayStats().RangeBytesRead - before; read != int64(selected[0].Length+selected[1].Length+2) {
		t.Fatalf("exact lookup scanned intervening transactions: bytes=%d", read)
	}
	var visited []Record
	if err := journal.VisitFrom(t.Context(), want[5].Location, want[6].Location.Cursor, func(record Record) error {
		record.Payload = append([]byte(nil), record.Payload...)
		visited = append(visited, record)
		return nil
	}); err != nil || !reflect.DeepEqual(visited, want[5:7]) {
		t.Fatalf("exact range changed: got=%#v error=%v", visited, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep valid JSON and the same file size so the read must validate the
	// canonical transaction checksum, not just parse JSON or check offsets.
	corrupt := bytes.Replace(data, []byte(`"n":1,`), []byte(`"n":9,`), 1)
	if bytes.Equal(corrupt, data) {
		t.Fatal("fixture did not contain the selected transaction")
	}
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.ReadLocations(t.Context(), selected[:1]); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("exact lookup did not reject changed canonical content: %v", err)
	}
}

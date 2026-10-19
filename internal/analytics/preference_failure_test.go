package analytics

import (
	"path/filepath"
	"testing"
)

func TestDisableFencesCollectionEvenWhenPersistenceFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	s, c := openCollector(t, path)
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	run := s.BeginRun(Automation, Native, Scheduled)
	// A directory is an invalid state file on every supported host, including
	// Windows where chmod does not reliably simulate a read-only file.
	s.path = t.TempDir()
	if _, err := s.SetEnabled(false); err == nil {
		t.Fatal("expected state write failure")
	}
	if s.Preferences().Enabled {
		t.Fatal("collection stayed enabled after disabling failed to persist")
	}
	run.Finish(Success)
	s.Activity(Game)
	if len(s.queue) > 0 || len(c.snapshot()) > 0 {
		t.Fatal("activity escaped the failed preference write")
	}
	s.path = path
	if _, err := s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	stored, err := loadState(path)
	if err != nil || stored.Enabled {
		t.Fatalf("retry did not persist disabled preference: %+v %v", stored, err)
	}
}

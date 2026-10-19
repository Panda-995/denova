package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"denova/internal/agents/conversationjournal"
	"denova/internal/localfs"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestStoreDeleteDistinguishesJournalFailureFromCleanupFailure(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup_failure=%v", cleanupFailure), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"keep", "target"} {
				if _, err := store.GetOrCreate(id); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			journalPath := store.sessionPath("target")
			blockedPath := journalPath
			if cleanupFailure {
				blockedPath = conversationjournal.SidecarPath(journalPath)
			}
			if err := os.Rename(blockedPath, blockedPath+".backup"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(blockedPath, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(blockedPath, "block-removal"), []byte("blocked"), 0o600); err != nil {
				t.Fatal(err)
			}
			artifactPath := sessionToolArtifactDirectory(journalPath)
			if err := os.Mkdir(artifactPath, 0o700); err != nil {
				t.Fatal(err)
			}
			err = store.Delete("target")
			if err == nil || errors.Is(err, ErrDeletionCleanup) != cleanupFailure {
				t.Fatalf("delete error = %v, committed cleanup failure = %v", err, cleanupFailure)
			}
			if cleanupFailure {
				if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
					t.Fatalf("canonical journal was not removed: %v", err)
				}
				if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
					t.Fatalf("index failure prevented remaining cleanup: %v", err)
				}
			} else if _, err := os.Stat(artifactPath); err != nil {
				t.Fatalf("failed canonical deletion removed artifacts: %v", err)
			}
			if !store.Exists("keep") {
				t.Fatal("deletion failure removed the sibling conversation")
			}
		})
	}
}

func TestStoreDeleteWaitsForCanonicalJournalLease(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOrCreate("keep"); err != nil {
		t.Fatal(err)
	}
	target, err := store.GetOrCreate("delete-me")
	if err != nil {
		t.Fatal(err)
	}
	release, err := localfs.AcquireLease(context.Background(), target.filePath+".domain.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	result := make(chan error, 1)
	runSessionErrorTestGoroutine(result, "delete leased session", func() error {
		return store.Delete(target.ID)
	})
	select {
	case err := <-result:
		t.Fatalf("delete crossed the held journal lease: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestStoreDeleteByPrefixWaitsForEveryCanonicalJournalLease(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.GetOrCreate("story-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOrCreate("story-b"); err != nil {
		t.Fatal(err)
	}
	release, err := localfs.AcquireLease(context.Background(), first.filePath+".domain.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	result := make(chan error, 1)
	runSessionErrorTestGoroutine(result, "delete session prefix", func() error {
		return store.DeleteByPrefix("story-")
	})
	select {
	case err := <-result:
		t.Fatalf("prefix delete crossed the held journal lease: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDeletedSessionHandleCannotAppendIntoRecreatedJournal(t *testing.T) {
	dir := t.TempDir()
	firstStore, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := firstStore.GetOrCreate("shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Append(agentschema.UserMessage("old journal")); err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.GetOrCreate("keep"); err != nil {
		t.Fatal(err)
	}

	secondStore, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondStore.Delete("shared"); err != nil {
		t.Fatal(err)
	}
	recreated, err := secondStore.GetOrCreate("shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.Append(agentschema.UserMessage("new journal")); err != nil {
		t.Fatal(err)
	}

	appendErr := stale.Append(agentschema.UserMessage("must not cross incarnation"))
	if appendErr == nil || !strings.Contains(appendErr.Error(), "incarnation") {
		t.Fatalf("stale append error = %v", appendErr)
	}
	reloadedStore, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := reloadedStore.Get("shared")
	if err != nil {
		t.Fatal(err)
	}
	messages := reloaded.GetMessages()
	if len(messages) != 1 || messages[0].Content != "new journal" {
		t.Fatalf("recreated journal was contaminated: %#v", messages)
	}
}

func runSessionErrorTestGoroutine(destination chan<- error, scope string, run func() error) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				destination <- fmt.Errorf("%s panic: %v", scope, recovered)
			}
		}()
		destination <- run()
	}()
}

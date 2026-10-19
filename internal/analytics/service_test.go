package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type collector struct {
	mu     sync.Mutex
	events []event
	bodies []string
}

func openCollector(t *testing.T, path string) (*Service, *collector) {
	t.Helper()
	c := &collector{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Mozilla/5.0 Denova/1.0" {
			t.Errorf("unexpected user agent")
		}
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Error(err)
		}
		var e event
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Error(err)
		}
		c.mu.Lock()
		c.events = append(c.events, e)
		c.bodies = append(c.bodies, string(raw))
		c.mu.Unlock()
		_, _ = w.Write([]byte(`{"cache":"test-receipt","sessionId":"test-session"}`))
	}))
	t.Cleanup(server.Close)
	s, err := Open(Options{Version: "0.6.0", StatePath: path, Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, c
}

func (c *collector) snapshot() []event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.events)
}

func awaitEvents(t *testing.T, c *collector, count int) []event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if events := c.snapshot(); len(events) >= count {
			return events
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected %d events, received %d", count, len(c.snapshot()))
	return nil
}

func TestNoticeDefaultAndHostWideActivityDeduplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	s, c := openCollector(t, path)
	if got := s.Preferences(); got != (Preferences{Enabled: true, Available: true}) {
		t.Fatalf("preferences=%+v", got)
	}
	s.Activity(Writing)
	if len(c.snapshot()) != 0 || len(s.queue) != 0 {
		t.Fatal("collected before the notice")
	}
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		s.Activity(Writing)
		s.Activity(Game)
	}
	events := awaitEvents(t, c, 6)
	wantNames := []string{"app.first_active", "app.active_day", "app.active_week", "app.active_month", "app.feature_active", "app.feature_active"}
	for i, e := range events {
		if e.Payload.Name != wantNames[i] {
			t.Fatalf("events=%+v", events)
		}
		if e.Payload.Data.Version != "0.6.0" || e.Payload.Data.Platform != runtime.GOOS {
			t.Fatalf("missing event metadata: %+v", e.Payload)
		}
		if e.Payload.Website != WebsiteID || e.Payload.Tag != "app" || e.Payload.Hostname != "app.denova-ai.com" || !strings.HasPrefix(e.Payload.ID, "app_") {
			t.Fatalf("payload=%+v", e.Payload)
		}
		if e.Payload.URL != "/app/writing" && e.Payload.URL != "/app/game" {
			t.Fatalf("unsafe URL=%s", e.Payload.URL)
		}
	}
	id := events[0].Payload.ID
	s.Close()
	restored, c2 := openCollector(t, path)
	if restored.state.ID != id {
		t.Fatal("identity changed across restart")
	}
	restored.Activity(Writing)
	restored.Activity(Game)
	if len(restored.queue) != 0 || len(c2.snapshot()) != 0 {
		t.Fatal("restart duplicated activity")
	}
}

func TestCalendarActivityAndExactRetention(t *testing.T) {
	s, c := openCollector(t, filepath.Join(t.TempDir(), "analytics.json"))
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	for _, days := range []int{0, 1, 1, 6, 7, 7, 8, 30, 30, 31} {
		s.mu.Lock()
		s.activityLocked(Writing, first.AddDate(0, 0, days))
		s.mu.Unlock()
	}
	// An event after all date mutations is a queue ordering barrier.
	s.mu.Lock()
	s.enqueueLocked(s.newEvent("app.action_completed", Projects, eventData{Action: BookCreated}, first))
	s.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	var events []event
	for time.Now().Before(deadline) {
		events = c.snapshot()
		if len(events) > 0 && events[len(events)-1].Payload.Name == "app.action_completed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Payload.Name]++
		if e.Payload.Data.Version != "0.6.0" || e.Payload.Data.Platform != runtime.GOOS {
			t.Fatalf("missing event metadata: %+v", e.Payload)
		}
		if strings.HasPrefix(e.Payload.Name, "app.retained_") && e.Payload.Data.CohortDate != "2026-12-31" {
			t.Fatalf("cohort=%s", e.Payload.Data.CohortDate)
		}
	}
	for _, name := range []string{"app.first_active", "app.retained_d1", "app.retained_d7", "app.retained_d30"} {
		if counts[name] != 1 {
			t.Fatalf("%s count=%d; all=%v", name, counts[name], counts)
		}
	}
	if counts["app.active_day"] != 7 || counts["app.active_week"] != 3 || counts["app.active_month"] != 2 {
		t.Fatalf("calendar counts=%v", counts)
	}
}

func TestCompletedActionsReportTheirActualScope(t *testing.T) {
	s, c := openCollector(t, filepath.Join(t.TempDir(), "analytics.json"))
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	s.ActionCompleted(BookCreated)
	s.ActionCompleted(ContentExported)
	s.ActionCompleted(VersionRestored)
	var actions []string
	for _, e := range awaitEvents(t, c, 10) {
		if e.Payload.Name == "app.action_completed" {
			actions = append(actions, string(e.Payload.Data.Action)+":"+e.Payload.URL)
		}
	}
	want := []string{"book_created:/app/projects", "content_exported:/app/writing", "version_restored:/app/versions"}
	if !slices.Equal(actions, want) {
		t.Fatalf("actions=%v, want %v", actions, want)
	}
}

func TestRunPayloadRecordsNumericMillisecondsIncludingZero(t *testing.T) {
	s, c := openCollector(t, filepath.Join(t.TempDir(), "analytics.json"))
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	for i, duration := range []time.Duration{42 * time.Millisecond, 6 * time.Minute, 25 * time.Hour} {
		run := s.BeginRun(Automation, Codex, Scheduled)
		run.started = time.Now().Add(-duration)
		run.Finish(Success)
		elapsed := time.Since(run.started).Milliseconds()
		e := awaitEvents(t, c, i+1)[i]
		var data map[string]any
		encoded, err := json.Marshal(e.Payload.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &data); err != nil {
			t.Fatal(err)
		}
		ms, ok := data["duration_ms"].(float64)
		if !ok || ms < float64(duration.Milliseconds()) || ms > float64(elapsed) {
			t.Fatalf("invalid execution duration: %s", encoded)
		}
		delete(data, "duration_ms")
		want := map[string]any{"version": "0.6.0", "platform": runtime.GOOS, "feature": "automation", "runtime": "codex", "trigger": "automation", "outcome": "success"}
		if !reflect.DeepEqual(data, want) {
			t.Fatalf("unexpected execution fields: %s", encoded)
		}
	}
	// An execution shorter than one millisecond must still include its duration.
	var zero eventData
	if err := json.Unmarshal([]byte(`{"duration_ms":0}`), &zero); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(zero)
	if err != nil || string(encoded) != `{"duration_ms":0}` {
		t.Fatalf("zero duration was omitted: %s, error=%v", encoded, err)
	}
}

func TestBackgroundRunsDoNotCreateUserActivityAndHaveBoundedPayloads(t *testing.T) {
	s, c := openCollector(t, filepath.Join(t.TempDir(), "analytics.json"))
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	run := s.BeginRun(Automation, Native, Scheduled)
	run.Finish(Failure)
	run.Finish(Failure)
	events := awaitEvents(t, c, 1)
	if len(events) != 1 || len(s.queue) != 0 {
		t.Fatal("duplicate run result")
	}
	want := eventData{Version: "0.6.0", Platform: runtime.GOOS, Feature: Automation, Runtime: Native, Trigger: Scheduled, Outcome: Failure, DurationMS: events[0].Payload.Data.DurationMS}
	if events[0].Payload.Data != want || events[0].Payload.Name != "app.run_finished" || s.state.FirstActive != "" {
		t.Fatalf("events=%+v; state=%+v", events, s.state)
	}
	var payload map[string]json.RawMessage
	encoded, _ := json.Marshal(events[0].Payload)
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 8 {
		t.Fatalf("unapproved payload keys=%s", encoded)
	}
	for _, key := range []string{"website", "hostname", "tag", "id", "url", "name", "timestamp", "data"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
}

type cancelTransport struct {
	started   chan struct{}
	cancelled chan struct{}
	once      sync.Once
}

func (transport *cancelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.once.Do(func() { close(transport.started) })
	<-request.Context().Done()
	close(transport.cancelled)
	return nil, request.Context().Err()
}

func TestDisableCancelsDeliveryAndInvalidatesOldRuns(t *testing.T) {
	transport := &cancelTransport{started: make(chan struct{}), cancelled: make(chan struct{})}
	s, err := Open(Options{Version: "0.6.0", StatePath: filepath.Join(t.TempDir(), "analytics.json"), Client: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	run := s.BeginRun(Writing, Native, User)
	select {
	case <-transport.started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	if _, err = s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.cancelled:
	case <-time.After(time.Second):
		t.Fatal("delivery was not cancelled")
	}
	s.Activity(Game)
	if _, err = s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	run.Finish(Success)
	if len(s.queue) != 0 {
		t.Fatal("disabled generation was replayed")
	}
	if _, err = s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	stored, err := loadState(s.path)
	if err != nil || stored.Enabled || !stored.NoticeSeen {
		t.Fatalf("disabled preference was not persisted: %+v %v", stored, err)
	}
}

func TestSourceBuildsAndUnreadableStateNeverCollect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	s, err := Open(Options{Version: "dev", StatePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = s.SetEnabled(true)
	s.Activity(Writing)
	if _, err = os.Stat(path); !os.IsNotExist(err) || s.Preferences().Available || len(s.queue) > 0 {
		t.Fatal("source build accessed host statistics")
	}
	if err = os.WriteFile(path, []byte("invalid preserved state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(Options{Version: "0.6.0", StatePath: path}); err == nil {
		t.Fatal("corrupt state was silently overwritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "invalid preserved state" {
		t.Fatal("corrupt state changed")
	}
}

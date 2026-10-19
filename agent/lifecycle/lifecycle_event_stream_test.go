package lifecycle

import (
	"testing"

	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
)

func TestPublishLatestEventReportsBoundedLoss(t *testing.T) {
	events := make(chan agentevent.Event, 2)
	var drops eventDropState
	publishLatestEvent(events, agentevent.Event{Cursor: 1, Payload: agentevent.AssistantDelta{Delta: "one"}}, &drops)
	publishLatestEvent(events, agentevent.Event{Cursor: 2, Payload: agentevent.AssistantDelta{Delta: "two"}}, &drops)
	publishLatestEvent(events, agentevent.Event{Cursor: 3, Payload: agentevent.AssistantDelta{Delta: "three"}}, &drops)

	gapEvent := <-events
	gap, ok := gapEvent.Payload.(agentevent.EventStreamGap)
	if !ok || gap.Dropped != 2 || gap.ResumeAfter != 2 {
		t.Fatalf("gap = %#v", gapEvent)
	}
	latest := <-events
	if latest.Cursor != 3 {
		t.Fatalf("latest event = %#v", latest)
	}
}

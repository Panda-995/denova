package lifecycle

import (
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
)

// eventDropState coalesces bounded observer loss without coupling a slow
// display consumer to authoritative Agent execution.
type eventDropState struct {
	count int
	after agentevent.Cursor
}

func publishLatestEvent(output chan agentevent.Event, event agentevent.Event, drops *eventDropState) {
	if output == nil || cap(output) == 0 {
		return
	}
	if drops == nil {
		drops = &eventDropState{}
	}
	needed := 1
	for {
		if drops.count > 0 && cap(output) >= 2 {
			needed = 2
		}
		if len(output)+needed <= cap(output) {
			break
		}
		select {
		case evicted := <-output:
			recordDroppedEvent(drops, evicted)
		default:
			// The consumer freed capacity between len and receive.
		}
	}
	if drops.count > 0 && cap(output) >= 2 {
		output <- agentevent.Event{
			Cursor: event.Cursor, RunID: event.RunID,
			Payload: agentevent.EventStreamGap{Dropped: drops.count, ResumeAfter: drops.after},
		}
		drops.count, drops.after = 0, 0
	}
	output <- event
}

func recordDroppedEvent(drops *eventDropState, event agentevent.Event) {
	if gap, ok := event.Payload.(agentevent.EventStreamGap); ok {
		drops.count += gap.Dropped
		if gap.ResumeAfter > drops.after {
			drops.after = gap.ResumeAfter
		}
		return
	}
	drops.count++
	if event.Cursor > drops.after {
		drops.after = event.Cursor
	}
}

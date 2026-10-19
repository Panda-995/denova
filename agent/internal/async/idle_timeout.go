package async

import (
	"context"
	"sync"
	"time"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

type idleActivityContextKey struct{}

// startIdleTimeout creates a resettable no-output deadline. Activity callbacks
// are non-blocking; stop joins the single recovered watcher so a completed
// modelToolLoop never leaves timer goroutines behind.
func StartIdleTimeout(parent context.Context, duration time.Duration) (context.Context, func(), func()) {
	ctx, cancel := context.WithCancelCause(parent)
	activity := make(chan struct{}, 1)
	done := make(chan struct{})
	var doneOnce sync.Once
	finish := func() { doneOnce.Do(func() { close(done) }) }
	touch := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	ctx = context.WithValue(ctx, idleActivityContextKey{}, touch)
	SafeGo(func() {
		defer finish()
		timer := time.NewTimer(duration)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(duration)
			case <-timer.C:
				cancel(agentschema.ErrIdleTimeout)
				return
			}
		}
	}, func(err error) {
		cancel(err)
		finish()
	})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel(context.Canceled)
			<-done
		})
	}
	return ctx, touch, stop
}

func IdleActivityFromContext(ctx context.Context) func() {
	if ctx == nil {
		return nil
	}
	activity, _ := ctx.Value(idleActivityContextKey{}).(func())
	return activity
}

func TouchIdleActivity(ctx context.Context) {
	if activity := IdleActivityFromContext(ctx); activity != nil {
		activity()
	}
}

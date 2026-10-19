package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestSubscriptionStreamShutdown(t *testing.T) {
	for _, phase := range []string{"idle", "blocked writer", "already shutting down"} {
		t.Run(phase, func(t *testing.T) {
			shutdown, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "already shutting down" {
				cancel()
			}
			ctx := context.WithValue(context.Background(), shutdownContextKey{}, shutdown)
			var released atomic.Int32
			c := app.NewContext(0)
			writer := NewSubscriptionStream(ctx, c, func() { released.Add(1) })
			t.Cleanup(func() { _ = c.Response.CloseBodyStream() })
			writeDone := make(chan error, 1)
			if phase == "blocked writer" {
				go func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							writeDone <- fmt.Errorf("writer panic: %v", recovered)
						}
					}()
					_, err := writer.Write([]byte("pending event"))
					writeDone <- err
				}()
			}
			cancel()
			if phase == "blocked writer" {
				select {
				case err := <-writeDone:
					if !errors.Is(err, io.ErrClosedPipe) {
						t.Fatalf("shutdown write: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("shutdown did not unblock writer")
				}
			}
			// Shutdown closes the write side, so a waiting reader receives EOF
			// and Hertz can send the final chunk instead of truncating the response.
			if _, err := c.Response.BodyStream().Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("shutdown read: %v", err)
			}
			if err := c.Response.CloseBodyStream(); err != nil {
				t.Fatal(err)
			}
			if got := released.Load(); got != 1 {
				t.Fatalf("subscription released %d times", got)
			}
		})
	}
}

func TestSubscriptionStreamTransportCloseReleasesSubscription(t *testing.T) {
	shutdown, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := context.WithValue(context.Background(), shutdownContextKey{}, shutdown)
	var released atomic.Int32
	c := app.NewContext(0)
	writer := NewSubscriptionStream(ctx, c, func() { released.Add(1) })
	if err := c.Response.CloseBodyStream(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := writer.Write([]byte("event after disconnect")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("disconnected write: %v", err)
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("subscription released %d times", got)
	}
}

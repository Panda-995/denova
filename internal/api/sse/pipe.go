package sse

import (
	"context"
	"io"
	"log/slog"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
)

type shutdownContextKey struct{}

// ShutdownMiddleware gives subscription streams a server shutdown signal without
// canceling ordinary requests or the detached tasks those requests may start.
func ShutdownMiddleware(shutdown context.Context) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(context.WithValue(ctx, shutdownContextKey{}, shutdown))
	}
}

// NewSubscriptionStream installs a subscription response body. The producer must
// close the returned writer when finished; Hertz owns the reader and its cleanup.
// Shutdown sends EOF and releases the subscription, including when a producer
// is idle or blocked on a write. It does not cancel the underlying Agent task.
func NewSubscriptionStream(ctx context.Context, c *app.RequestContext, unsubscribe func()) *io.PipeWriter {
	reader, writer := io.Pipe()
	body := &subscriptionBody{PipeReader: reader, writer: writer, unsubscribe: unsubscribe}
	shutdown, ok := ctx.Value(shutdownContextKey{}).(context.Context)
	if !ok {
		shutdown = context.Background()
	}
	body.stop = context.AfterFunc(shutdown, func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(ctx, "sse_shutdown_failed", "error", recovered)
			}
		}()
		body.finish()
	})
	c.Response.Header.Set("Content-Type", "text/event-stream")
	c.Response.Header.Set("Cache-Control", "no-cache")
	// Hertz decides keep-alive before reading a streaming response. Closing the
	// connection after EOF avoids entering another keep-alive read during shutdown.
	c.Response.Header.Set("Connection", "close")
	c.Response.ImmediateHeaderFlush = true
	c.Response.SetBodyStream(body, -1)
	return writer
}

type subscriptionBody struct {
	*io.PipeReader
	writer      *io.PipeWriter
	unsubscribe func()
	stop        func() bool
	once        sync.Once
}

func (body *subscriptionBody) finish() {
	body.once.Do(func() {
		_ = body.writer.Close()
		body.unsubscribe()
	})
}

func (body *subscriptionBody) Close() error {
	body.stop()
	body.finish()
	return body.PipeReader.Close()
}

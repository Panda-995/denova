package async

import (
	"fmt"
	"runtime/debug"
)

// PanicError converts a recovered goroutine or extension panic into an error.
type PanicError struct {
	Value any
	Stack []byte
}

func (err *PanicError) Error() string {
	return fmt.Sprintf("panic recovered: %v", err.Value)
}

func RecoveredPanic(value any) *PanicError {
	return &PanicError{Value: value, Stack: append([]byte(nil), debug.Stack()...)}
}

func SafeGo(run func(), onPanic func(error)) {
	go func() {
		defer func() {
			if value := recover(); value != nil {
				func() {
					defer func() { _ = recover() }()
					onPanic(RecoveredPanic(value))
				}()
			}
		}()
		run()
	}()
}

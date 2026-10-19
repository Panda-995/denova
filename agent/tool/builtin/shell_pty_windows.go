//go:build windows

package builtin

import (
	"github.com/charmbracelet/x/xpty"
)

func preparePTYAfterStart(xpty.Pty) {}

func finishPTYAfterWait(terminal xpty.Pty) { _ = terminal.Close() }

//go:build !windows

package builtin

import (
	"github.com/charmbracelet/x/xpty"
)

func preparePTYAfterStart(terminal xpty.Pty) {
	if unixPTY, ok := terminal.(*xpty.UnixPty); ok {
		_ = unixPTY.Slave().Close()
	}
}

func finishPTYAfterWait(xpty.Pty) {}

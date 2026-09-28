//go:build !windows

package ui

import (
	"os"
	"syscall"
)

// hangup tells p its terminal closed.
func hangup(p *os.Process) { _ = p.Signal(syscall.SIGHUP) }

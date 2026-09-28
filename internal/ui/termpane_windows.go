package ui

import "os"

// hangup ends p: Windows has no SIGHUP.
func hangup(p *os.Process) { _ = p.Kill() }

package ui

import (
	"errors"
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// An embedded terminal: a child process on a pty, drawn through a vt
// emulator into the panel (agent_panel.go runs herdr agent attach in one).
// Output is parsed on a reader goroutine; the model only learns that
// something changed (termOutputMsg) and re-renders on its own loop.

// termSpec is what to run in an embedded terminal.
type termSpec struct {
	title string
	argv  []string
	env   []string // added to the environment laneway runs with
	dir   string
}

// termSession is one running terminal.
type termSession struct {
	title string

	emu  *vt.SafeEmulator
	pty  *os.File
	cmd  *exec.Cmd
	wmu  sync.Mutex // serialises writes to pty (keys vs emulator replies)
	w, h int

	bracketed    atomic.Bool  // child enabled bracketed paste (DECSET 2004)
	mouse        atomic.Int32 // mouse reporting mode the child enabled (DECSET 1000/1002/1003), 0 = off
	mouseSGR     atomic.Bool  // …in SGR encoding (DECSET 1006)
	cursorHidden atomic.Bool  // child hid the cursor (DECTCEM)
	dirty        chan struct{}
	done         chan struct{} // closed once the child has exited
	exitErr      error

	// Render cache: View runs on every keystroke, the screen changes far less.
	stale    atomic.Bool
	rendered string
}

// termOutputMsg says a session drew something; termExitMsg that it ended.
type termOutputMsg struct{ t *termSession }
type termExitMsg struct{ t *termSession }

// termFrame caps how often a busy child repaints the panel.
const termFrame = 16 * time.Millisecond

func startTerm(spec termSpec, w, h int) (*termSession, error) {
	if len(spec.argv) == 0 {
		return nil, errors.New(i18n.T("no command"))
	}
	w, h = max(w, 10), max(h, 3)
	cmd := exec.Command(spec.argv[0], spec.argv[1:]...)
	cmd.Dir = spec.dir
	cmd.Env = append(termEnviron(), spec.env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		return nil, err
	}
	t := &termSession{
		title: spec.title,
		emu:   vt.NewSafeEmulator(w, h),
		pty:   f,
		cmd:   cmd,
		w:     w,
		h:     h,
		dirty: make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	t.stale.Store(true)
	t.emu.SetCallbacks(vt.Callbacks{
		EnableMode:       func(mode ansi.Mode) { t.setMode(mode, true) },
		DisableMode:      func(mode ansi.Mode) { t.setMode(mode, false) },
		CursorVisibility: func(visible bool) { t.cursorHidden.Store(!visible) },
	})
	go t.readLoop()
	go t.replyLoop()
	return t, nil
}

// termEnviron is laneway's environment minus what would confuse the child:
// our own TERM (the emulator is xterm-ish, whatever we run in) and Claude
// Code's nesting guard when laneway itself was started from Claude.
func termEnviron() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "TERM", "COLORTERM", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_CHILD_SESSION":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func (t *termSession) setMode(mode ansi.Mode, on bool) {
	switch mode {
	case ansi.ModeBracketedPaste:
		t.bracketed.Store(on)
	case ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		n := int32(mode.Mode())
		if on {
			t.mouse.Store(n)
		} else {
			t.mouse.CompareAndSwap(n, 0)
		}
	case ansi.ModeMouseExtSgr:
		t.mouseSGR.Store(on)
	}
}

// wheel sends a wheel notch at the 0-based cell (x, y), when the child asked
// for mouse reports. Reports whether it was sent.
func (t *termSession) wheel(up bool, x, y int) bool {
	b := ansi.MouseWheelDown
	if up {
		b = ansi.MouseWheelUp
	}
	return t.mouseEvent(ansi.EncodeMouseButton(b, false, false, false, false), x, y, false)
}

// wantsMouse reports whether the child takes mouse reports.
func (t *termSession) wantsMouse() bool {
	return t.mouse.Load() != 0 && !t.exited()
}

// wantsDrag reports whether the child takes motion while a button is held.
func (t *termSession) wantsDrag() bool {
	mode := t.mouse.Load()
	return (mode == 1002 || mode == 1003) && !t.exited()
}

// mouseEvent sends an encoded mouse report at the 0-based cell (x, y). A
// release in X10 encoding carries no button; SGR keeps it and flags release.
func (t *termSession) mouseEvent(b byte, x, y int, release bool) bool {
	if !t.wantsMouse() {
		return false
	}
	if t.mouseSGR.Load() {
		t.write(ansi.MouseSgr(b, x, y, release))
	} else if x < 223 && y < 223 {
		if release {
			b |= 0b11
		}
		t.write(ansi.MouseX10(b, x, y))
	}
	return true
}

func (t *termSession) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			_, _ = t.emu.Write(buf[:n])
			t.stale.Store(true)
			select {
			case t.dirty <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
	t.exitErr = t.cmd.Wait()
	_ = t.pty.Close()
	// Ends replyLoop's Read. Not emu.Close: it flips a flag Read checks
	// unlocked, a data race; closing the pipe is safe from any goroutine.
	if pw, ok := t.emu.InputPipe().(*io.PipeWriter); ok {
		_ = pw.CloseWithError(io.EOF)
	}
	close(t.done)
}

// replyLoop forwards the emulator's answers to the child's queries (cursor
// position, device attributes) back into the pty.
func (t *termSession) replyLoop() {
	buf := make([]byte, 1024)
	for {
		n, err := t.emu.Read(buf)
		if n > 0 {
			t.write(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// waitTermOutput blocks until the session draws or exits.
func waitTermOutput(t *termSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-t.dirty:
			time.Sleep(termFrame) // let a burst land in one frame
			return termOutputMsg{t}
		case <-t.done:
			return termExitMsg{t}
		}
	}
}

func (t *termSession) exited() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *termSession) write(s string) {
	if s == "" || t.exited() {
		return
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, _ = io.WriteString(t.pty, s)
}

// paste types text as one paste, so its newlines don't submit line by line.
func (t *termSession) paste(s string) {
	if t.bracketed.Load() {
		s = ansi.BracketedPasteStart + s + ansi.BracketedPasteEnd
	}
	t.write(s)
}

func (t *termSession) sendKey(k tea.KeyPressMsg) {
	t.write(encodeKey(k))
}

func (t *termSession) resize(w, h int) {
	w, h = max(w, 10), max(h, 3)
	if w == t.w && h == t.h || t.exited() {
		return
	}
	t.w, t.h = w, h
	t.emu.Resize(w, h)
	_ = pty.Setsize(t.pty, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	t.stale.Store(true)
}

// view is the screen as styled lines, re-rendered only after new output.
func (t *termSession) view() string {
	if t.stale.Swap(false) {
		t.rendered = t.emu.Render()
	}
	return t.rendered
}

// stop asks the child to end (SIGHUP, as a closed terminal would) and kills it
// if it hasn't within a few seconds.
func (t *termSession) stop() {
	if t.exited() || t.cmd.Process == nil {
		return
	}
	hangup(t.cmd.Process)
	go func() {
		select {
		case <-t.done:
		case <-time.After(3 * time.Second):
			_ = t.cmd.Process.Kill()
		}
	}()
}

func (t *termSession) exitStatus() string {
	if t.exitErr == nil {
		return i18n.T("exited")
	}
	return i18n.Tf("exited: %v", t.exitErr)
}

// encodeKey turns a key press into the bytes a legacy xterm sends for it —
// what the vt emulator advertises, so what the child expects to parse.
func encodeKey(k tea.KeyPressMsg) string {
	mod := k.Mod
	alt := ""
	if mod.Contains(tea.ModAlt) {
		alt = "\x1b"
		mod &^= tea.ModAlt
	}
	ctrl := mod.Contains(tea.ModCtrl)
	shift := mod.Contains(tea.ModShift)
	if k.Text != "" && !ctrl {
		return alt + k.Text
	}
	// xterm modifier parameter: 1 + shift(1) + alt(2) + ctrl(4).
	param := 1
	if shift {
		param++
	}
	if alt != "" {
		param += 2
	}
	if ctrl {
		param += 4
	}
	csi := func(final string) string {
		if param == 1 {
			return "\x1b[" + final
		}
		return fmt.Sprintf("\x1b[1;%d%s", param, final)
	}
	tilde := func(n int) string {
		if param == 1 {
			return fmt.Sprintf("\x1b[%d~", n)
		}
		return fmt.Sprintf("\x1b[%d;%d~", n, param)
	}
	switch k.Code {
	case tea.KeyEnter:
		if shift || alt != "" {
			return "\x1b\r" // what Claude Code and most readlines take as a newline
		}
		return "\r"
	case tea.KeyTab:
		if shift {
			return "\x1b[Z"
		}
		return alt + "\t"
	case tea.KeyBackspace:
		if ctrl {
			return alt + "\x08"
		}
		return alt + "\x7f"
	case tea.KeyEscape:
		return alt + "\x1b"
	case tea.KeySpace:
		if ctrl {
			return alt + "\x00"
		}
		return alt + " "
	case tea.KeyUp:
		return csi("A")
	case tea.KeyDown:
		return csi("B")
	case tea.KeyRight:
		return csi("C")
	case tea.KeyLeft:
		return csi("D")
	case tea.KeyHome:
		return csi("H")
	case tea.KeyEnd:
		return csi("F")
	case tea.KeyInsert:
		return tilde(2)
	case tea.KeyDelete:
		return tilde(3)
	case tea.KeyPgUp:
		return tilde(5)
	case tea.KeyPgDown:
		return tilde(6)
	case tea.KeyF1, tea.KeyF2, tea.KeyF3, tea.KeyF4:
		letter := string(rune('P' + k.Code - tea.KeyF1))
		if param == 1 {
			return "\x1bO" + letter
		}
		return csi(letter)
	}
	if k.Code >= tea.KeyF5 && k.Code <= tea.KeyF12 {
		return tilde([]int{15, 17, 18, 19, 20, 21, 23, 24}[k.Code-tea.KeyF5])
	}
	if ctrl {
		switch c := k.Code; {
		case c >= 'a' && c <= 'z':
			return alt + string(rune(c-'a'+1))
		case c >= '[' && c <= '_': // [ \ ] ^ _
			return alt + string(rune(c-'['+0x1b))
		case c == '@' || c == '2':
			return alt + "\x00"
		case c == '/' || c == '-':
			return alt + "\x1f"
		}
		return ""
	}
	if k.Code > 0 && k.Code < 0x110000 && k.Code < tea.KeyExtended {
		return alt + string(k.Code)
	}
	return ""
}

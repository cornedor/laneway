package ui

import (
	"slices"

	"charm.land/lipgloss/v2"
)

// brailleCanvas plots dots at 2×4 per terminal cell with Unicode braille.
type brailleCanvas struct {
	w, h  int // in cells
	cells []rune
}

func newBraille(w, h int) *brailleCanvas {
	c := &brailleCanvas{w: max(w, 1), h: max(h, 1)}
	c.cells = make([]rune, c.w*c.h)
	for i := range c.cells {
		c.cells[i] = 0x2800
	}
	return c
}

// brailleBits is the dot bit for a column (0, 1) and row (0–3) in a cell.
var brailleBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// dots is the canvas size in dots.
func (c *brailleCanvas) dots() (int, int) { return c.w * 2, c.h * 4 }

// set lights the dot at x, y (0,0 top left); off-canvas dots are dropped.
func (c *brailleCanvas) set(x, y int) {
	if x < 0 || y < 0 || x >= c.w*2 || y >= c.h*4 {
		return
	}
	c.cells[(y/4)*c.w+x/2] |= brailleBits[x%2][y%4]
}

// line draws from (x0, y0) to (x1, y1); every step-th dot when dotted.
func (c *brailleCanvas) line(x0, y0, x1, y1, step int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for i := 0; ; i++ {
		if step <= 1 || i%step == 0 {
			c.set(x0, y0)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// rows are the canvas lines, blank cells as spaces.
func (c *brailleCanvas) rows() []string {
	out := make([]string, c.h)
	for r := range c.h {
		line := slices.Clone(c.cells[r*c.w : (r+1)*c.w])
		for i, ch := range line {
			if ch == 0x2800 {
				line[i] = ' '
			}
		}
		out[r] = string(line)
	}
	return out
}

// overlay draws fg over bg, the two the same size: a cell with any of fg's
// dots in fgStyle (bg's dots merged in), else bg's in bgStyle.
func overlay(fg, bg *brailleCanvas, fgStyle, bgStyle lipgloss.Style) []string {
	return layered([]*brailleCanvas{fg, bg}, []lipgloss.Style{fgStyle, bgStyle})
}

// layered draws canvases of one size over each other, the first on top: a
// cell takes the style of the first canvas with a dot in it, and the dots
// of all.
func layered(cs []*brailleCanvas, styles []lipgloss.Style) []string {
	top := cs[0]
	out := make([]string, top.h)
	for r := range top.h {
		var line []rune
		run := -1
		flush := func(to *string) {
			if len(line) == 0 {
				return
			}
			st := styles[len(styles)-1] // blanks as the bottom canvas
			if run >= 0 {
				st = styles[run]
			}
			*to += st.Render(string(line))
			line = line[:0]
		}
		for i := range top.w {
			var ch rune = 0x2800
			on := -1
			for k, c := range cs {
				if d := c.cells[r*top.w+i]; d != 0x2800 {
					ch |= d
					if on < 0 {
						on = k
					}
				}
			}
			if on != run {
				flush(&out[r])
				run = on
			}
			if ch == 0x2800 {
				ch = ' '
			}
			line = append(line, ch)
		}
		flush(&out[r])
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

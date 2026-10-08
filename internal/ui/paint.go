package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Rendering works on plain strings of styled lines. Widths are always cell
// widths (ansi.StringWidth), never len.

func width(s string) int { return ansi.StringWidth(s) }

func sgrBG(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// paint gives every cell of s the background bg and pads (or cuts) each line
// to w cells. Spans that set their own background keep it because bg is
// re-applied after every reset. lipgloss.NoColor leaves the terminal's own
// background.
func paint(s string, w int, bg color.Color) string {
	if _, ok := bg.(lipgloss.NoColor); ok {
		return fit(s, w, len(strings.Split(s, "\n")))
	}
	code := sgrBG(bg)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lw := width(l)
		if lw > w {
			l = ansi.Truncate(l, w, "")
			lw = width(l)
		}
		l = strings.ReplaceAll(l, "\x1b[m", "\x1b[m"+code)
		l = strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+code)
		lines[i] = code + l + strings.Repeat(" ", w-lw) + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// pad pads s with spaces to w cells, cutting it with an ellipsis if longer.
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lw := width(s); lw > w {
		return ansi.Truncate(s, w, "…")
	} else {
		return s + strings.Repeat(" ", w-lw)
	}
}

// padLeft right-aligns s in w cells.
func padLeft(s string, w int) string {
	if lw := width(s); lw < w {
		return strings.Repeat(" ", w-lw) + s
	}
	return ansi.Truncate(s, w, "…")
}

// trunc cuts s to w cells with an ellipsis.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// truncLeft keeps the end of s (a path's file name) within w cells.
func truncLeft(s string, w int) string {
	if width(s) <= w {
		return s
	}
	if w <= 1 {
		return ""
	}
	return "…" + ansi.TruncateLeft(s, width(s)-w+1, "")
}

// spread puts left and right on one line of w cells.
func spread(left, right string, w int) string {
	gap := w - width(left) - width(right)
	if gap < 1 {
		return pad(left, w-width(right)-1) + " " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// fit pads or cuts s to exactly h lines of w cells.
func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = pad(l, w)
	}
	return strings.Join(lines, "\n")
}

// columns lays out blocks side by side with the given widths and gaps of
// gap cells, every block padded to h lines.
func columns(h, gap int, blocks []string, widths []int) string {
	split := make([][]string, len(blocks))
	for i, b := range blocks {
		split[i] = strings.Split(fit(b, widths[i], h), "\n")
	}
	var out []string
	sp := strings.Repeat(" ", gap)
	for r := 0; r < h; r++ {
		var row []string
		for i := range blocks {
			row = append(row, split[i][r])
		}
		out = append(out, strings.Join(row, sp))
	}
	return strings.Join(out, "\n")
}

// wrap word-wraps s to w cells.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	out := lipgloss.NewStyle().Width(w).Render(s)
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// lines splits s into lines (nil for "").
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// box draws a rounded box of outer width w around rows. A row equal to
// boxRule draws a separator. Every inner cell gets background bg.
func box(rows []string, w int, border, bg color.Color) string {
	inner := w - 4
	b := fg(border)
	var out []string
	out = append(out, b.Render("╭"+strings.Repeat("─", w-2)+"╮"))
	for _, r := range rows {
		if r == boxRule {
			out = append(out, b.Render("├"+strings.Repeat("─", w-2)+"┤"))
			continue
		}
		out = append(out, b.Render("│")+paint(" "+pad(r, inner)+" ", w-2, bg)+b.Render("│"))
	}
	out = append(out, b.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

const boxRule = "\x00rule"

// overlay draws fg over a dimmed copy of bg at column x, row y. The backdrop
// is re-rendered as plain faint text (design: no shadows, dim the
// background).
func overlay(bg, fgs string, x, y, w int) string {
	dim := func(s string) string { return paint(sFaint.Render(s), width(s), cBG) }
	bl := strings.Split(ansi.Strip(bg), "\n")
	fl := strings.Split(fgs, "\n")
	for i := range bl {
		bl[i] = pad(bl[i], w)
	}
	for i, l := range fl {
		r := y + i
		if r < 0 || r >= len(bl) {
			continue
		}
		lw := width(l)
		left := ansi.Truncate(bl[r], x, "")
		right := ansi.TruncateLeft(bl[r], x+lw, "")
		bl[r] = dim(left) + l + dim(right)
	}
	for i, l := range bl {
		if !strings.Contains(l, "\x1b") {
			bl[i] = dim(l)
		}
	}
	return strings.Join(bl, "\n")
}

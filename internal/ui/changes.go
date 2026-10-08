package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Cflex96/ghostui/internal/config"
)

// changesView lists the unsaved changes: the review overlay (s), and the
// dialog quitting opens while there are any. Whatever is left in it when
// the human saves is what gets saved.
type changesView struct {
	cur, top int
	quit     bool
}

func (a *App) openChanges(quit bool) {
	a.chg = changesView{quit: quit}
	a.ov = ovChanges
}

// change is one unsaved change with the exact config lines it touches.
type change struct {
	key            string
	before, after  []string // saved and working values
	removed, added []string // config lines
	line           int      // first line of key in the saved file, 0 if new
}

func (a *App) changes() []change {
	work := a.working()
	var out []change
	for _, k := range a.unsaved() {
		c := change{key: k, before: a.file.Values(k), after: a.vals(k)}
		c.removed, c.added = diffValues(entryLines(a.file, k), entryLines(work, k))
		if e := a.file.Entries(k); len(e) > 0 {
			c.line = e[0] + 1
		}
		out = append(out, c)
	}
	return out
}

func entryLines(f *config.File, key string) []string {
	var out []string
	for _, i := range f.Entries(key) {
		out = append(out, strings.TrimSpace(f.Lines[i].Raw))
	}
	return out
}

func (a *App) changesKey(msg tea.KeyPressMsg) tea.Cmd {
	c := &a.chg
	keys := a.unsaved()
	c.cur = clamp(c.cur, 0, len(keys)-1)
	switch msg.String() {
	case "up", "k", "ctrl+p":
		c.cur = max(0, c.cur-1)
	case "down", "j", "ctrl+n":
		c.cur = clamp(c.cur+1, 0, len(keys)-1)
	case "x", "backspace", "delete":
		if c.cur < len(keys) {
			k := keys[c.cur]
			return tea.Batch(a.set(k, a.file.Values(k)), a.flash(k+" reverted · u undoes it", stInfo))
		}
	case "enter", "s", "ctrl+s":
		if c.quit {
			a.quitting = true
		} else {
			a.ov = ovNone
		}
		return a.save()
	case "d":
		if c.quit {
			a.quitting = true
		} else {
			a.ov = ovNone
		}
		return a.discard()
	case "esc":
		a.ov, a.quitting = ovNone, false
	case "q":
		if !c.quit {
			a.ov = ovNone
		}
	}
	return nil
}

func (a *App) viewChanges() string {
	c := &a.chg
	w := min(clamp(a.w-6, 40, 84), a.w)
	inner := w - 4
	cs := a.changes()
	c.cur = clamp(c.cur, 0, len(cs)-1)

	title := "unsaved changes"
	if c.quit {
		title = "save before quitting?"
	}
	count := fmt.Sprintf("%d · ", len(cs))
	sub := count + truncLeft(tilde(a.opt.Path), max(0, inner-width(title)-width(count)-2))
	rows := []string{spread(sAccB.Render(title), sFaint.Render(sub), inner), boxRule}

	var body []string
	selFrom, selTo := 0, 0
	for i, ch := range cs {
		sel := i == c.cur
		marker := "  "
		if sel {
			marker = sAccent.Render("▌") + " "
		}
		where := sFaint.Render("new")
		if ch.line > 0 {
			where = sFaint.Render(fmt.Sprintf("line %d", ch.line))
		}
		head := spread(marker+trunc(describeChange(ch.key, ch.before, ch.after), inner-width(where)-4), where, inner)
		if sel {
			head = paint(head, inner, cRaised)
			selFrom = len(body)
		}
		body = append(body, head)
		for _, l := range ch.removed {
			body = append(body, "    "+sStrike.Render(trunc("- "+l, inner-4)))
		}
		for _, l := range ch.added {
			body = append(body, "    "+sOK.Render(trunc("+ "+l, inner-4)))
		}
		if sel {
			selTo = len(body)
		}
	}
	if len(cs) == 0 {
		body = []string{sMuted.Render("No unsaved changes.")}
	}

	note := "Nothing is written until you save."
	if a.opt.Preview {
		note = "Ghostty shows these now; discarding puts the saved config back."
	}
	n := clamp(len(body), 1, max(1, a.h-9))
	top := c.top
	if selFrom < top {
		top = selFrom
	}
	if selTo > top+n {
		top = selTo - n
	}
	c.top = clamp(top, 0, max(0, len(body)-n))
	rows = append(rows, body[c.top:min(len(body), c.top+n)]...)
	if a.h >= 16 {
		rows = append(rows, "", sFaint.Render(trunc(note, inner)))
	}
	rows = append(rows, boxRule, a.hintLine(a.changesHints(len(cs) > 0), inner))
	return box(rows, w, cBorder, cSurface)
}

func (a *App) changesHints(any bool) []hint {
	if !a.chg.quit {
		if !any {
			return []hint{{k: "esc", label: "close", right: true}}
		}
		return []hint{{k: "⏎", label: "save"}, {k: "x", label: "revert"}, {k: "d", label: "discard all"}, {k: "↑↓", label: "move"}, {k: "esc", label: "close", right: true}}
	}
	if !any {
		return []hint{{k: "⏎", label: "quit"}, {k: "esc", label: "keep editing", right: true}}
	}
	return []hint{{k: "⏎", label: "save & quit"}, {k: "d", label: "discard & quit"}, {k: "x", label: "revert"}, {k: "esc", label: "keep editing", right: true}}
}

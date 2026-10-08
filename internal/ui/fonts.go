package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Cflex96/ghostui/internal/fuzzy"
	"github.com/Cflex96/ghostui/internal/ghostty"
)

// fontPicker searches `ghostty +list-fonts` for a font-family option:
// ⏎ replaces the primary font, ⇥ adds a fallback.
type fontPicker struct {
	o   *ghostty.Option
	q   input
	res []fontRes
	cur int
	top int
}

type fontRes struct {
	name  string
	idx   []int
	score int
}

func (a *App) openFonts(o *ghostty.Option) {
	a.fp = fontPicker{o: o}
	a.fp.refresh(a)
	a.ov = ovFonts
}

func (f *fontPicker) refresh(a *App) {
	q := strings.TrimSpace(f.q.Value())
	cur := a.list(f.o.Key)
	var res []fontRes
	for _, name := range a.fonts {
		if q == "" {
			s := 0
			if slices.Contains(cur, name) {
				s = 1
			}
			res = append(res, fontRes{name: name, score: s})
		} else if m, ok := fuzzy.Find(q, name); ok {
			res = append(res, fontRes{name: name, idx: m.Indices, score: m.Score})
		}
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].score > res[j].score })
	f.res, f.cur, f.top = res, 0, 0
}

func (a *App) fontKey(msg tea.KeyPressMsg) tea.Cmd {
	f := &a.fp
	key := f.o.Key
	l := slices.Clone(a.list(key))
	pick := ""
	if f.cur >= 0 && f.cur < len(f.res) {
		pick = f.res[f.cur].name
	}
	switch msg.String() {
	case "esc":
		a.ov = ovNone
		return nil
	case "up", "ctrl+p":
		f.cur = max(0, f.cur-1)
		return nil
	case "down", "ctrl+n":
		f.cur = clamp(f.cur+1, 0, len(f.res)-1)
		return nil
	case "pgup":
		f.cur = max(0, f.cur-10)
		return nil
	case "pgdown":
		f.cur = clamp(f.cur+10, 0, len(f.res)-1)
		return nil
	case "enter":
		if pick == "" {
			return nil
		}
		// pick replaces the primary; the other fallbacks stay.
		l = slices.DeleteFunc(l, func(s string) bool { return s == pick })
		if len(l) == 0 {
			l = []string{pick}
		} else {
			l[0] = pick
		}
		a.ov = ovNone
		return tea.Batch(a.set(key, l), a.flash(key+" → "+pick, stOK))
	case "tab":
		if pick == "" || slices.Contains(l, pick) {
			return nil
		}
		return tea.Batch(a.set(key, append(l, pick)), a.flash("fallback + "+pick, stOK))
	case "ctrl+x":
		if len(l) > 1 {
			return a.set(key, l[:len(l)-1])
		}
		return nil
	}
	if f.q.Handle(msg, true) {
		f.refresh(a)
	}
	return nil
}

func (a *App) viewFonts() string {
	f := &a.fp
	w := min(clamp(a.w-6, 40, 72), a.w)
	inner := w - 4
	l := a.list(f.o.Key)
	current := sFaint.Render("system default")
	if len(l) > 0 {
		current = sText.Render(l[0])
		if len(l) > 1 {
			current += sFaint.Render("  + " + strings.Join(l[1:], ", "))
		}
	}
	count := fmt.Sprintf("%d fonts", len(f.res))
	if a.fonts == nil {
		count = "loading…"
	}
	rows := []string{
		spread(sAccB.Render(f.o.Key), sFaint.Render("primary + fallbacks"), inner),
		trunc(current, inner),
		boxRule,
		spread(sAccB.Render("›")+" "+f.q.View("type to filter fonts", true, inner-14), sFaint.Render(count), inner),
		boxRule,
	}
	n := clamp(a.h-14, 3, 14)
	f.top = scrollTo(f.top, f.cur, n, len(f.res))
	for i := f.top; i < len(f.res) && i < f.top+n; i++ {
		r := f.res[i]
		sel := i == f.cur
		base := sSoft
		marker := "  "
		if sel {
			base, marker = sText, sAccent.Render("▌")+" "
		}
		line := marker + lipgloss.StyleRunes(r.name, r.idx, base.Foreground(cAccent).Bold(true), base)
		if slices.Contains(l, r.name) {
			tag := "fallback"
			if l[0] == r.name {
				tag = "primary"
			}
			line = spread(line, sFaint.Render(tag), inner)
		}
		if sel {
			line = paint(line, inner, cRaised)
		}
		rows = append(rows, line)
	}
	for i := len(f.res); i < n; i++ {
		rows = append(rows, "")
	}
	hints := []hint{{k: "⏎", label: "use as primary"}, {k: "⇥", label: "add fallback"}, {k: "⌃x", label: "drop last fallback"}, {k: "esc", label: "close", right: true}}
	rows = append(rows, boxRule, a.hintLine(hints, inner))
	return box(rows, w, cBorder, cSurface)
}

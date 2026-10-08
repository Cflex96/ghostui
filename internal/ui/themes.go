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

const (
	modeDark = iota
	modeLight
	modeBoth
)

var modeNames = []string{"◐ dark", "○ light", "◑ both"}

type themeStudio struct {
	mode      int
	q         input
	filtering bool
	list      []*ghostty.Theme
	idx       map[string][]int
	cur, top  int
	pinned    *ghostty.Theme
}

func (a *App) themeValue() ghostty.ThemeValue {
	if o := a.schema.Get("theme"); o != nil {
		if v := a.cur(o); v != "" {
			return ghostty.ParseThemeValue(v)
		}
	}
	return ghostty.ThemeValue{}
}

// enter opens the studio on the theme currently in use. A light/dark pair
// starts in dark mode; a single theme in "both".
func (t *themeStudio) enter(a *App) {
	t.mode = modeBoth
	if a.themeValue().Pair() {
		t.mode = modeDark
	}
	t.q.Set("")
	t.filtering, t.pinned = false, nil
	t.refresh(a)
	t.focusCurrent(a)
	t.top = max(0, t.cur-6)
}

func (t *themeStudio) target(a *App) string {
	tv := a.themeValue()
	if t.mode == modeLight {
		return tv.Light
	}
	return tv.Dark
}

func (t *themeStudio) focusCurrent(a *App) {
	name := t.target(a)
	for i, th := range t.list {
		if th.Name == name {
			t.cur = i
			return
		}
	}
	t.cur = 0
}

func (t *themeStudio) refresh(a *App) {
	if a.schema == nil {
		return
	}
	var keep string
	if t.cur < len(t.list) {
		keep = t.list[t.cur].Name
	}
	q := strings.TrimSpace(t.q.Value())
	type hit struct {
		t     *ghostty.Theme
		score int
		idx   []int
	}
	var hits []hit
	for _, th := range a.themes {
		if (t.mode == modeDark && !th.Dark) || (t.mode == modeLight && th.Dark) {
			continue
		}
		if q == "" {
			hits = append(hits, hit{t: th})
		} else if m, ok := fuzzy.Find(q, th.Name); ok {
			hits = append(hits, hit{t: th, score: m.Score, idx: m.Indices})
		}
	}
	if q != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	}
	t.list, t.idx = nil, map[string][]int{}
	t.cur = 0
	for i, h := range hits {
		t.list = append(t.list, h.t)
		t.idx[h.t.Name] = h.idx
		if h.t.Name == keep && q == "" {
			t.cur = i
		}
	}
}

func (t *themeStudio) selected() *ghostty.Theme {
	if t.cur >= 0 && t.cur < len(t.list) {
		return t.list[t.cur]
	}
	return nil
}

func (a *App) applyTheme() tea.Cmd {
	t := &a.th
	sel := t.selected()
	if sel == nil {
		return nil
	}
	tv := a.themeValue()
	switch t.mode {
	case modeDark:
		tv.Dark = sel.Name
		if tv.Light == "" {
			tv.Light = sel.Name
		}
	case modeLight:
		tv.Light = sel.Name
		if tv.Dark == "" {
			tv.Dark = sel.Name
		}
	default:
		tv = ghostty.ThemeValue{Light: sel.Name, Dark: sel.Name}
	}
	if tv.String() == a.cur(a.schema.Get("theme")) {
		return a.flash("already in use", stInfo)
	}
	return a.set("theme", []string{tv.String()})
}

func (a *App) themeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	t := &a.th
	s := msg.String()
	move := func(d int) { t.cur = clamp(t.cur+d, 0, len(t.list)-1) }
	switch s {
	case "up", "ctrl+p":
		move(-1)
		return nil, true
	case "down", "ctrl+n":
		move(1)
		return nil, true
	case "pgup":
		move(-10)
		return nil, true
	case "pgdown":
		move(10)
		return nil, true
	case "enter":
		t.filtering = false
		return a.applyTheme(), true
	}
	if t.filtering {
		switch s {
		case "esc":
			if t.q.Value() != "" {
				t.q.Set("")
			} else {
				t.filtering = false
			}
			t.refresh(a)
			return nil, true
		case "tab", "shift+tab":
			return nil, false
		}
		if t.q.Handle(msg, true) {
			t.refresh(a)
		}
		return nil, true
	}
	switch s {
	case "home":
		move(-len(t.list))
	case "end":
		move(len(t.list))
	case "space":
		if sel := t.selected(); sel != nil && t.pinned != sel {
			t.pinned = sel
		} else {
			t.pinned = nil
		}
	case "d", "l", "b":
		t.mode = map[string]int{"d": modeDark, "l": modeLight, "b": modeBoth}[s]
		t.refresh(a)
		t.focusCurrent(a)
	case "/":
		t.filtering = true
	case "esc":
		if t.pinned != nil {
			t.pinned = nil
			return nil, true
		}
		if t.q.Value() != "" {
			t.q.Set("")
			t.refresh(a)
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
	return nil, true
}

func (a *App) viewThemes() string {
	var modes []string
	for i, m := range modeNames {
		if i == a.th.mode {
			modes = append(modes, sSeg.Render(" "+m+" "))
		} else {
			modes = append(modes, sMuted.Render(" "+m+" "))
		}
	}
	hints := []hint{{k: "↑↓", label: "browse"}, {k: "⏎", label: "apply"}, {k: "space", label: "pin & compare"},
		{k: "d l b", label: "dark · light · both"}, {k: "/", label: "filter"}, {k: "esc", label: "back", right: true}}
	if a.th.filtering {
		hints = []hint{{k: "↑↓", label: "browse"}, {k: "⏎", label: "apply"}, {k: "esc", label: "clear filter", right: true}}
	}
	return a.frame([]string{"settings", "themes"}, strings.Join(modes, ""), false, hints, a.themesBody)
}

func (a *App) themesBody(w, h int) string {
	if a.themes == nil {
		return sFaint.Render("reading ghostty +list-themes …")
	}
	switch {
	case w >= 96:
		lw := 36
		return columns(h, 3, []string{a.themeList(lw, h), a.themePreviews(w-lw-3, h)}, []int{lw, w - lw - 3})
	case w >= 70:
		lw := 30
		return columns(h, 2, []string{a.themeList(lw, h), a.themePreviews(w-lw-2, h)}, []int{lw, w - lw - 2})
	}
	// Narrow: list on top, preview below; short screens get the list only
	// (each row still carries its color strip).
	if h < 22 {
		return a.themeList(w, h)
	}
	ph := min(h/2, 16)
	return a.themeList(w, h-ph) + "\n" + a.themePreviews(w, ph)
}

func (a *App) themeList(w, h int) string {
	t := &a.th
	var filter string
	switch {
	case t.filtering:
		filter = t.q.View("type to filter", true, w-14)
	case t.q.Value() != "":
		filter = sMuted.Render(t.q.Value())
	default:
		filter = sFaint.Render("/ to filter")
	}
	fbox := box([]string{spread(sAccB.Render("›")+" "+filter, sFaint.Render(fmt.Sprintf("%d", len(t.list))), w-4)}, w, cActive, cSurface)

	tv := a.themeValue()
	cur := sMuted.Render(trunc(orUnset(tv.String()), w))
	if tv.Pair() {
		cur = sMuted.Render(trunc("light:"+tv.Light+", dark:"+tv.Dark, w))
	}
	foot := []string{"", sFaint.Render("current"), cur}
	if h < 16 {
		foot = nil
	}

	listH := h - 3 - 1 - len(foot)
	tall := listH >= 16
	per := 1
	if tall {
		per = 2
	}
	n := max(1, listH/per)
	t.top = scrollTo(t.top, t.cur, n, len(t.list))
	var out []string
	out = append(out, strings.Split(fbox, "\n")...)
	out = append(out, "")
	if len(t.list) == 0 {
		out = append(out, sFaint.Render("no theme matches"))
	}
	for i := t.top; i < len(t.list) && i < t.top+n; i++ {
		th := t.list[i]
		sel := i == t.cur
		marker := "  "
		name := sMuted
		if sel {
			marker, name = sAccent.Render("▌")+" ", sText.Bold(true)
		}
		var tags []string
		if th.Name == tv.Dark && th.Name == tv.Light {
			tags = append(tags, "in use")
		} else if th.Name == tv.Dark {
			tags = append(tags, "dark")
		} else if th.Name == tv.Light {
			tags = append(tags, "light")
		}
		if t.pinned == th {
			tags = append(tags, "pinned")
		}
		var strip []string
		for _, c := range th.Swatch() {
			strip = append(strip, swatch(c, "▀▀"))
		}
		label := lipgloss.StyleRunes(th.Name, t.idx[th.Name], name.Foreground(cAccent).Bold(true), name)
		line1 := spread(marker+label, sFaint.Render(strings.Join(tags, " · ")), w)
		if tall {
			line2 := "  " + strings.Join(strip, " ")
			if sel {
				line1, line2 = paint(line1, w, cRaised), paint(line2, w, cRaised)
			}
			out = append(out, line1, line2)
		} else {
			compact := ""
			for _, c := range th.Swatch() {
				compact += swatch(c, "█")
			}
			line := spread(marker+trunc(label, w-12), compact, w)
			if sel {
				line = paint(line, w, cRaised)
			}
			out = append(out, line)
		}
	}
	for len(out) < h-len(foot) {
		out = append(out, "")
	}
	return strings.Join(append(out[:h-len(foot)], foot...), "\n")
}

func (a *App) themePreviews(w, h int) string {
	t := &a.th
	sel := t.selected()
	if sel == nil {
		return ""
	}
	legendH := 3
	if h < 14 {
		legendH = 0
	}
	ph := h - legendH
	if t.pinned != nil && t.pinned != sel {
		if w >= 90 {
			pw := (w - 2) / 2
			return columns(ph, 2, []string{a.themePreview(t.pinned, "pinned", pw, ph), a.themePreview(sel, a.previewTag(sel), w-pw-2, ph)}, []int{pw, w - pw - 2}) + a.legend(sel, w, legendH)
		}
		half := ph / 2
		return a.themePreview(t.pinned, "pinned", w, half) + "\n" + a.themePreview(sel, a.previewTag(sel), w, ph-half) + a.legend(sel, w, legendH)
	}
	return a.themePreview(sel, a.previewTag(sel), w, ph) + a.legend(sel, w, legendH)
}

func (a *App) previewTag(th *ghostty.Theme) string {
	tv := a.themeValue()
	if th.Name == tv.Dark || th.Name == tv.Light {
		if !slices.Equal(a.vals("theme"), a.file.Values("theme")) {
			return "● in use · unsaved"
		}
		return "● in use"
	}
	return "preview · not applied yet"
}

// themePreview renders sample shell, git, test and code output in th's
// colors.
func (a *App) themePreview(th *ghostty.Theme, tag string, w, h int) string {
	c := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	p := th.Palette
	fgc, dim := c(th.FG), c(p[8])
	red, green, yellow, blue, magenta, cyan := c(p[1]), c(p[2]), c(p[3]), c(p[4]), c(p[5]), c(p[6])
	prompt := blue.Render("~/code/ghostui") + " " + magenta.Render("main") + " "
	inner := w - 4
	sample := []string{
		spread(dim.Render(th.Name), dim.Render(tag), inner),
		"",
		prompt + green.Render("❯") + fgc.Render(" git status"),
		fgc.Render("On branch main"),
		fgc.Render("Changes not staged for commit:"),
		red.Render("        modified:   internal/ui/palette.go"),
		red.Render("        modified:   internal/config/parse.go"),
		"",
		prompt + yellow.Render("±2") + " " + green.Render("❯") + fgc.Render(" go test ./..."),
		green.Render("ok  ") + fgc.Render("    internal/config   ") + dim.Render("0.41s"),
		red.Render("FAIL") + fgc.Render("    internal/ui       ") + dim.Render("0.20s"),
		"",
		dim.Render("// match on doc text, not just keys"),
		magenta.Render("func") + " " + blue.Render("Match") + fgc.Render("(q ") + yellow.Render("string") + fgc.Render(") []") + cyan.Render("Option") + fgc.Render(" {"),
	}
	more := [][]string{{
		"",
		prompt + green.Render("❯") + fgc.Render(" ls"),
		blue.Render("cmd/  design/  internal/  ") + fgc.Render("go.mod  go.sum  ") + green.Render("ghostui*"),
	}, {
		"",
		prompt + green.Render("❯") + fgc.Render(" git diff --stat"),
		fgc.Render(" internal/ui/palette.go   | 12 ") + green.Render("++++++++") + red.Render("----"),
		fgc.Render(" internal/config/parse.go |  4 ") + green.Render("++") + red.Render("--"),
		dim.Render(" 2 files changed, 10 insertions(+), 6 deletions(-)"),
	}}
	for _, m := range more {
		if len(sample)+len(m)+2 <= h-2 {
			sample = append(sample, m...)
		}
	}
	sample = append(sample, "", green.Render("❯")+" "+lipgloss.NewStyle().Background(lipgloss.Color(th.FG)).Render(" "))
	rows := make([]string, 0, max(0, h-2))
	for i := 0; i < h-2; i++ {
		if i < len(sample) {
			rows = append(rows, trunc(sample[i], inner))
		} else {
			rows = append(rows, "")
		}
	}
	return box(rows, w, cBorder, lipgloss.Color(th.BG))
}

func (a *App) legend(th *ghostty.Theme, w, h int) string {
	if h == 0 {
		return ""
	}
	names := []string{"bg", "fg", "red", "green", "yellow", "blue", "magenta", "cyan"}
	cols := []string{th.BG, th.FG, th.Palette[1], th.Palette[2], th.Palette[3], th.Palette[4], th.Palette[5], th.Palette[6]}
	cw := max(4, (w-7)/8)
	var top, bot []string
	for i := range names {
		top = append(top, swatch(cols[i], strings.Repeat("█", cw)))
		bot = append(bot, sMuted.Render(pad(names[i], cw)))
	}
	return "\n\n" + strings.Join(top, " ") + "\n" + strings.Join(bot, " ")
}

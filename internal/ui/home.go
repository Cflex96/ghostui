package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Cflex96/ghostui/internal/ghostty"
	"github.com/Cflex96/ghostui/internal/state"
)

// ghostPixels is the ghost from the design (Main.dc.html) as a bitmap. The
// mockup draws each pixel two columns wide and half a row tall; terminal
// cells are about twice as tall as wide, so that comes out twice as wide as
// the ghost is. ghostLines draws square pixels instead.
var ghostPixels = []string{
	"....####....",
	"..########..",
	".##########.",
	"############",
	"##..####..##",
	"##..####..##",
	"############",
	"############",
	"############",
	"############",
	"###.####.###",
	"##...##...##",
}

// ghostLines renders the ghost with half blocks: one column and half a row
// per pixel.
func ghostLines() []string {
	on := func(x, y int) bool { return y < len(ghostPixels) && ghostPixels[y][x] == '#' }
	var out []string
	for y := 0; y < len(ghostPixels); y += 2 {
		var b strings.Builder
		for x := 0; x < len(ghostPixels[0]); x++ {
			switch top, bot := on(x, y), on(x, y+1); {
			case top && bot:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bot:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		out = append(out, b.String())
	}
	return out
}

type row struct {
	o   *ghostty.Option // nil for the "add setting" row
	add bool
}

// overrides lists the keys set in the file (plus unsaved new ones), in file
// order.
func (a *App) overrides() []string {
	keys := a.file.Keys()
	var extra []string
	for _, m := range []map[string][]string{a.inflight, a.pending} {
		for k := range m {
			if !slices.Contains(keys, k) && !slices.Contains(extra, k) {
				extra = append(extra, k)
			}
		}
	}
	sort.Strings(extra)
	var out []string
	for _, k := range append(keys, extra...) {
		if a.overridden(k) {
			out = append(out, k)
		}
	}
	return out
}

// rows are the list for the current tab: the overrides on the overview,
// otherwise every option in the category that applies to this OS.
func (a *App) rows() []row {
	var rs []row
	if a.tab == tabOverview {
		for _, k := range a.overrides() {
			rs = append(rs, row{o: a.option(k)})
		}
		return append(rs, row{add: true})
	}
	cat := tabs[a.tab].name
	for _, o := range a.schema.Options {
		if o.Category == cat && !o.Irrelevant() && o.Key != "command-palette-entry" {
			rs = append(rs, row{o: o})
		}
	}
	return rs
}

func (a *App) selected() *ghostty.Option {
	rs := a.rows()
	if c := a.cursor[a.tab]; c >= 0 && c < len(rs) {
		return rs[c].o
	}
	return nil
}

func (a *App) viewHome() string {
	_, iw := a.margins()
	o := a.selected()
	hints := []hint{{k: "/", label: "search"}, {k: "↑↓", label: "move"}}
	if o != nil {
		switch o.Kind {
		case ghostty.KBool:
			hints = append(hints, hint{k: "⏎", label: "toggle"})
		case ghostty.KFlags:
			hints = append(hints, hint{k: "←→", label: "pick"}, hint{k: "space", label: "toggle"})
		case ghostty.KTheme, ghostty.KKeybind, ghostty.KFont:
			hints = append(hints, hint{k: "⏎", label: "open"})
		default:
			if a.adjustable(o) {
				hints = append(hints, hint{k: "←→", label: "adjust"})
			}
			hints = append(hints, hint{k: "⏎", label: "edit"})
		}
		if a.overridden(o.Key) && o.Kind != ghostty.KKeybind {
			hints = append(hints, hint{k: "⌫", label: "reset"})
		}
	} else {
		hints = append(hints, hint{k: "⏎", label: "add"})
	}
	if a.dirty() {
		hints = append(hints, hint{k: "⌃s", label: "save"})
	}
	hints = append(hints, hint{k: "u", label: "undo"}, hint{k: "?", label: "help"})
	return a.frame([]string{"settings"}, a.homeStatus(iw-22), true, hints, a.homeBody)
}

func (a *App) homeBody(w, h int) string {
	const leftW, gap = 30, 5
	if w >= 104 && h >= 14 {
		rw := w - leftW - gap
		return columns(h, gap, []string{a.homeLeft(leftW, h), a.homeRight(rw, h)}, []int{leftW, rw})
	}
	return a.homeRight(w, h)
}

func (a *App) homeLeft(w, h int) string {
	var out []string
	if h >= 24 {
		out = append(out, "")
		for _, l := range ghostLines() {
			out = append(out, sAccent.Render(l))
		}
		out = append(out, "", "")
	}
	out = append(out, sBold.Render("Make it yours."))
	for _, l := range wrap("Every option, its docs and its default, one keystroke away. No more digging through the config reference.", w) {
		out = append(out, sMuted.Render(l))
	}
	if un := a.unsaved(); len(un) > 0 {
		out = append(out, "", "", spread(label("unsaved"), sAccent.Render(fmt.Sprint(len(un))), w), "")
		room := max(1, min(5, h-len(out)-2))
		for i := len(un) - 1; i >= 0 && len(un)-1-i < room; i-- {
			k := un[i]
			out = append(out, trunc(describeChange(k, a.file.Values(k), a.vals(k)), w))
		}
		if len(un) > room {
			out = append(out, sFaint.Render(fmt.Sprintf("+ %d more", len(un)-room)))
		}
		out = append(out, sMuted.Render("s")+sFaint.Render(" review · ")+sMuted.Render("⌃s")+sFaint.Render(" save"))
		return strings.Join(out, "\n")
	}
	out = append(out, "", "", label("recent"), "")
	n := max(1, min(5, h-len(out)-2))
	for i := len(a.steps) - 1; i >= 0 && len(a.steps)-1-i < n; i-- {
		out = append(out, a.recentLine(a.steps[i], w))
	}
	if len(a.steps) == 0 {
		out = append(out, sFaint.Render("nothing yet"))
	} else {
		out = append(out, sMuted.Render("u")+sFaint.Render(" to undo the last change"))
	}
	return strings.Join(out, "\n")
}

func (a *App) recentLine(e state.Entry, w int) string {
	ago := sFaint.Render(since(e.Time))
	return spread(trunc(describeChange(e.Key, e.Before, e.After), w-width(ago)-1), ago, w)
}

func describeChange(key string, before, after []string) string {
	arrow := sOK.Render("→")
	if len(before) > 1 || len(after) > 1 || key == "keybind" || key == "palette" {
		removed, added := diffValues(before, after)
		switch {
		case len(added) > 0:
			return sText.Render(key) + " " + sOK.Render("+") + " " + sText.Render(added[0])
		case len(removed) > 0:
			return sText.Render(key) + " " + sErr.Render("−") + " " + sFaint.Render(removed[0])
		}
	}
	show := func(v []string) string {
		if len(v) == 0 {
			return "default"
		}
		return displayValue(key, v[len(v)-1])
	}
	if len(before) == 0 {
		return sText.Render(key) + " " + arrow + " " + sText.Render(show(after))
	}
	return sText.Render(key) + " " + sFaint.Render(show(before)) + " " + arrow + " " + sText.Render(show(after))
}

func diffValues(a, b []string) (onlyA, onlyB []string) {
	for _, v := range a {
		if !slices.Contains(b, v) {
			onlyA = append(onlyA, v)
		}
	}
	for _, v := range b {
		if !slices.Contains(a, v) {
			onlyB = append(onlyB, v)
		}
	}
	return
}

// displayValue shortens a value for one-line display.
func displayValue(key, v string) string {
	if key == "theme" {
		tv := ghostty.ParseThemeValue(v)
		if tv.Pair() {
			return tv.Light + " ◐ " + tv.Dark
		}
		return tv.Dark
	}
	if v != "" && strings.TrimSpace(v) == "" {
		return `"` + v + `"`
	}
	return v
}

func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (a *App) homeRight(w, h int) string {
	rs := a.rows()
	title := label("your overrides")
	if a.tab != tabOverview {
		title = label(tabs[a.tab].name) + sFaint.Render(fmt.Sprintf("  ·  %d options", len(rs)))
	}
	out := []string{spread(title, sFaint.Render(truncLeft(tilde(a.opt.Path), w-width(title)-2)), w), ""}
	if a.tab == tabOverview && len(a.problems) > 0 {
		out = append(out, sErr.Render("✗ ")+sSoft.Render(trunc(a.problems[0], w-2)))
	}

	docsH := 0
	switch {
	case h >= 26:
		docsH = 8
	case h >= 16:
		docsH = 6
	}
	listH := h - len(out) - docsH
	if docsH > 0 {
		listH-- // gap above the docs panel
	}
	listH = max(1, listH)

	cur := clamp(a.cursor[a.tab], 0, len(rs)-1)
	a.cursor[a.tab] = cur
	top := scrollTo(a.scroll[a.tab], cur, listH, len(rs))
	a.scroll[a.tab] = top

	keyW := 14
	for _, r := range rs {
		if r.o != nil {
			keyW = max(keyW, width(r.o.Key)+3)
		}
	}
	keyW = min(keyW, 28, w/2)

	if a.tab == tabOverview && len(rs) == 1 {
		out = append(out, sMuted.Render("No overrides yet. Ghostty is running on its defaults."), "")
		listH -= 2
	}
	for i := top; i < len(rs) && i < top+listH; i++ {
		out = append(out, a.renderRow(rs[i], w, keyW, i == cur))
	}
	for len(out) < h-docsH {
		out = append(out, "")
	}
	if docsH > 0 {
		if o := a.selected(); o != nil {
			out = append(out, a.docsPanel(o, w, docsH))
		} else {
			out = append(out, a.addPanel(w, docsH))
		}
	}
	return strings.Join(out, "\n")
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(v, hi))
}

// scrollTo returns the first visible row so that cur stays visible with a
// little context.
func scrollTo(top, cur, h, n int) int {
	margin := min(2, h/4)
	if cur < top+margin {
		top = cur - margin
	}
	if cur >= top+h-margin {
		top = cur - h + margin + 1
	}
	return clamp(top, 0, max(0, n-h))
}

func (a *App) renderRow(r row, w, keyW int, sel bool) string {
	marker := "  "
	if sel {
		marker = sAccent.Render("▌") + " "
	}
	if r.add {
		line := marker + sFaint.Render("+ add setting  ") + keycap("/")
		if sel {
			line = marker + sText.Render("+ add setting  ") + keycap("/")
			return paint(line, w, cRaised)
		}
		return line
	}
	o := r.o
	keyStyle := sMuted
	if sel {
		keyStyle = sText.Bold(true)
	}
	labelW := 7
	if w < 60 {
		labelW = 0
	}
	valueW := w - 2 - keyW - labelW
	val := a.widget(o, valueW-1, sel)
	if a.tab != tabOverview && a.overridden(o.Key) && width(val)+2 < valueW {
		val += " " + sAccent.Render("•")
	}
	line := marker + pad(keyStyle.Render(o.Key), keyW) + pad(val, valueW)
	if labelW > 0 {
		lbl := o.Label()
		st := sFaint
		if lbl == "unknown" {
			st = sErr
		}
		if _, ok := a.included(o.Key); ok {
			lbl, st = "include", sAccent
		}
		line += st.Render(padLeft(lbl, labelW))
	}
	if sel {
		return paint(line, w, cRaised)
	}
	return line
}

func (a *App) docsPanel(o *ghostty.Option, w, h int) string {
	inner := w - 4
	meta := o.Kind.String()
	if d := a.defaultText(o); d != "" {
		meta += " · default " + d
	}
	switch {
	case o.Irrelevant() && o.Platform == "linux":
		meta = "linux only · " + meta
	case o.Irrelevant():
		meta = "macOS only · " + meta
	}
	rows := []string{spread(sAccB.Render(o.Key), sFaint.Render(trunc(meta, inner/2)), inner)}
	if note := a.includeNote(o); note != "" {
		rows = append(rows, sAccent.Render(trunc(note, inner)))
	}
	var rel string
	if len(o.Related) > 0 {
		rel = sFaint.Render("related")
		for _, k := range o.Related {
			rel += "  " + sKeycap.Foreground(cMuted).Render(" "+k+" ")
		}
	}
	room := h - 2 - len(rows)
	if rel != "" {
		room--
	}
	body := clip(wrap(ghostty.Paragraphs(o.Docs, 3), inner), room, inner)
	for _, l := range body {
		rows = append(rows, sSoft.Render(l))
	}
	for len(rows) < h-2-boolInt(rel != "") {
		rows = append(rows, "")
	}
	if rel != "" {
		rows = append(rows, trunc(rel, inner))
	}
	return box(rows, w, cBorder, cSurface)
}

// includeNote says when an included file sets o too, or "".
func (a *App) includeNote(o *ghostty.Option) string {
	inc, ok := a.included(o.Key)
	if !ok {
		return ""
	}
	file := tilde(inc.Path)
	if rel, err := filepath.Rel(a.store.Dir(), inc.Path); err == nil && !strings.HasPrefix(rel, "..") {
		file = rel
	}
	if o.Repeatable {
		return "also set in " + file + ", which Ghostty loads after this file"
	}
	v := displayValue(o.Key, inc.Values[len(inc.Values)-1])
	if v == "" {
		v = `""`
	}
	return "⚠ Ghostty uses " + v + " from " + file + ", which it loads after this file"
}

// clip keeps at most n lines, ending on text with an ellipsis if it cut.
func clip(ls []string, n, w int) []string {
	if len(ls) <= n {
		return ls
	}
	ls = ls[:max(0, n)]
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	if len(ls) > 0 {
		ls[len(ls)-1] = trunc(ls[len(ls)-1]+" …", w)
	}
	return ls
}

func (a *App) addPanel(w, h int) string {
	rows := []string{sAccB.Render("add a setting"),
		sSoft.Render(trunc("Search every option by name or by what its docs say.", w-4))}
	for len(rows) < h-2 {
		rows = append(rows, "")
	}
	return box(rows, w, cBorder, cSurface)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (a *App) defaultText(o *ghostty.Option) string {
	switch o.Kind {
	case ghostty.KKeybind, ghostty.KPalette:
		return ""
	}
	d := o.Default()
	switch {
	case d == "":
		return "unset"
	case strings.TrimSpace(d) == "":
		return `"` + d + `"`
	}
	return trunc(d, 24)
}

func (a *App) homeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	rs := a.rows()
	n := len(rs)
	cur := clamp(a.cursor[a.tab], 0, n-1)
	var o *ghostty.Option
	if cur >= 0 && cur < n {
		o = rs[cur].o
	}
	move := func(d int) {
		a.cursor[a.tab] = clamp(cur+d, 0, n-1)
		a.flagCur = 0
	}
	s := msg.String()
	switch s {
	case "up", "ctrl+p":
		move(-1)
	case "down", "ctrl+n":
		move(1)
	case "pgup":
		move(-10)
	case "pgdown":
		move(10)
	case "home":
		move(-n)
	case "end":
		move(n)
	case "left", "right", "shift+left", "shift+right":
		if o == nil {
			return nil, true
		}
		dir := 1
		if strings.HasSuffix(s, "left") {
			dir = -1
		}
		if o.Kind == ghostty.KFlags {
			a.flagCur = clamp(a.flagCur+dir, 0, len(o.Flags)-1)
			return nil, true
		}
		return a.adjust(o, dir, strings.HasPrefix(s, "shift")), true
	case "space":
		if o == nil {
			return nil, true
		}
		if o.Kind == ghostty.KFlags {
			return a.toggleFlag(o, a.flagCur), true
		}
		return a.adjust(o, 1, false), true
	case "enter":
		if o == nil {
			a.openPalette("")
			return nil, true
		}
		return a.open(o), true
	case "backspace", "delete":
		if o == nil || !a.overridden(o.Key) {
			return nil, true
		}
		if o.Kind == ghostty.KKeybind {
			// Too easy to wipe every binding by accident; unbind them one by one.
			return a.flash("unbind keybinds one by one in keys (k)", stInfo), true
		}
		return tea.Batch(a.set(o.Key, nil), a.flash(o.Key+" reset to default · u undoes it", stInfo)), true
	default:
		return nil, false
	}
	return nil, true
}

// open starts editing o in the way that suits its type.
func (a *App) open(o *ghostty.Option) tea.Cmd {
	switch o.Kind {
	case ghostty.KTheme:
		return a.setTab(tabThemes)
	case ghostty.KKeybind:
		return a.setTab(tabKeys)
	case ghostty.KFont:
		a.openFonts(o)
		return nil
	case ghostty.KBool:
		return a.adjust(o, 1, false)
	}
	a.beginEdit(o)
	a.ov = ovEditor
	return nil
}

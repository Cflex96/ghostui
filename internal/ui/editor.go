package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Cflex96/ghostui/internal/fuzzy"
	"github.com/Cflex96/ghostui/internal/ghostty"
)

// editor is the value editor shared by the palette and the standalone edit
// overlay. Adjustments apply live as unsaved changes (and Ghostty previews
// them); esc puts the original value back.
type editor struct {
	o       *ghostty.Option
	orig    []string
	text    input
	texting bool
	item    int // selected entry of a list option
	adding  bool
}

func (a *App) beginEdit(o *ghostty.Option) {
	a.ed = editor{o: o, orig: slices.Clone(a.vals(o.Key))}
	a.flagCur = 0
	if a.textFirst(o) && a.ov != ovPalette {
		a.startText()
	}
}

// listKind reports whether o is edited as a list of entries.
func listKind(o *ghostty.Option) bool {
	switch o.Kind {
	case ghostty.KFont, ghostty.KKeybind, ghostty.KTheme:
		return false
	}
	return o.Repeatable
}

// textFirst reports whether editing o means typing a value.
func (a *App) textFirst(o *ghostty.Option) bool {
	if listKind(o) {
		return false
	}
	switch o.Kind {
	case ghostty.KColor, ghostty.KDuration:
		return true
	case ghostty.KString:
		return len(o.Values) == 0
	}
	return false
}

func (a *App) startText() {
	e := &a.ed
	e.texting, e.adding = true, false
	v := a.cur(e.o)
	if listKind(e.o) {
		if l := a.vals(e.o.Key); e.item < len(l) {
			v = l[e.item]
		} else {
			v, e.adding = "", true
		}
	}
	e.text.Set(v)
}

func (a *App) revertEdit() tea.Cmd {
	e := a.ed
	a.ed.texting = false
	if e.o == nil || slices.Equal(a.vals(e.o.Key), e.orig) {
		return nil
	}
	return tea.Batch(a.set(e.o.Key, e.orig), a.flash(e.o.Key+" put back", stInfo))
}

func (a *App) commitText() tea.Cmd {
	e := &a.ed
	v := e.text.Value()
	e.texting = false
	if listKind(e.o) {
		l := slices.Clone(a.vals(e.o.Key))
		if !e.adding && e.item >= len(l) {
			// The entry went away while it was typed (rejected, or edited
			// on disk): what was typed is a new entry.
			e.adding = true
		}
		switch {
		case e.adding && v != "":
			l = append(l, v)
			e.item = len(l) - 1
		case !e.adding && v == "":
			l = slices.Delete(l, e.item, e.item+1)
		case !e.adding:
			l[e.item] = v
		}
		return a.set(e.o.Key, l)
	}
	a.ov = ovNone
	if v == "" {
		return a.set(e.o.Key, nil)
	}
	return a.set(e.o.Key, []string{v})
}

// editKey handles keys shared by both editor surfaces. done closes the
// surface (commit); it returns handled=false for keys it doesn't use.
func (a *App) editKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	e := &a.ed
	o := e.o
	s := msg.String()
	if e.texting {
		switch s {
		case "esc":
			e.texting = false
			return nil, true
		case "enter":
			return a.commitText(), true
		}
		e.text.Handle(msg, true)
		return nil, true
	}
	switch s {
	case "left", "right", "shift+left", "shift+right":
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
		if o.Kind == ghostty.KFlags {
			return a.toggleFlag(o, a.flagCur), true
		}
		if a.adjustable(o) {
			return a.adjust(o, 1, false), true
		}
	case "backspace", "delete":
		if listKind(o) {
			if l := a.vals(o.Key); e.item < len(l) {
				return a.set(o.Key, slices.Delete(slices.Clone(l), e.item, e.item+1)), true
			}
			return nil, true
		}
		if !a.overridden(o.Key) {
			return nil, true
		}
		return a.set(o.Key, nil), true
	case "tab":
		if !listKind(o) && o.Kind != ghostty.KTheme && o.Kind != ghostty.KKeybind && o.Kind != ghostty.KFont {
			a.startText()
		}
		return nil, true
	case "+":
		if listKind(o) {
			e.item = len(a.vals(o.Key))
			a.startText()
			return nil, true
		}
	}
	if listKind(o) {
		switch s {
		case "up", "ctrl+p":
			e.item = max(0, e.item-1)
			return nil, true
		case "down", "ctrl+n":
			e.item = min(len(a.vals(o.Key)), e.item+1)
			return nil, true
		}
	}
	return nil, false
}

// enter performs ⏎ on the editor: open a dedicated screen, start typing,
// or keep the value and close.
func (a *App) editorEnter() tea.Cmd {
	o := a.ed.o
	switch o.Kind {
	case ghostty.KTheme:
		a.ov = ovNone
		return a.setTab(tabThemes)
	case ghostty.KKeybind:
		a.ov = ovNone
		return a.setTab(tabKeys)
	case ghostty.KFont:
		a.openFonts(o)
		return nil
	}
	if listKind(o) || a.textFirst(o) {
		a.startText()
		return nil
	}
	a.ov = ovNone
	if !slices.Equal(a.vals(o.Key), a.ed.orig) {
		return a.flash(o.Key+" → "+displayValue(o.Key, a.cur(o)), stOK)
	}
	return nil
}

// --- standalone editor overlay ----------------------------------------------

func (a *App) editorKey(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := a.editKey(msg); ok {
		return cmd
	}
	o := a.ed.o
	switch s := msg.String(); s {
	case "esc", "q":
		cmd := a.revertEdit()
		a.ov = ovNone
		return cmd
	case "enter":
		return a.editorEnter()
	default:
		// Typing a number starts entering one.
		if o.Kind.Numeric() && len(s) == 1 && strings.ContainsAny(s, "0123456789.-") {
			a.startText()
			a.ed.text.Set(s)
		}
	}
	return nil
}

func (a *App) viewEditor() string {
	w := min(78, a.w-4)
	inner := w - 4
	o := a.ed.o
	rows := []string{spread(sAccB.Render(o.Key), sFaint.Render(a.metaLine(o)), inner), boxRule}
	maxBody := max(4, a.h-8)
	rows = append(rows, a.editorBody(o, inner, maxBody)...)
	rows = append(rows, boxRule, a.hintLine(a.editorHints(false), inner))
	return box(rows, w, cBorder, cSurface)
}

func (a *App) metaLine(o *ghostty.Option) string {
	m := o.Kind.String()
	if d := a.defaultText(o); d != "" {
		m += " · default " + d
	}
	return m
}

func (a *App) editorHints(palette bool) []hint {
	o := a.ed.o
	esc := hint{k: "esc", label: "cancel", right: true}
	if palette {
		esc.label = "close"
		if a.pal.focus == focusEditor {
			esc.label = "back"
		}
	}
	if o == nil {
		return []hint{{k: "↑↓", label: "move"}, esc}
	}
	if a.ed.texting {
		return []hint{{k: "⏎", label: "set"}, {k: "esc", label: "cancel", right: true}}
	}
	var hs []hint
	if palette {
		hs = append(hs, hint{k: "↑↓", label: "move"})
	}
	switch {
	case o.Kind == ghostty.KTheme:
		hs = append(hs, hint{k: "⏎", label: "theme studio"})
	case o.Kind == ghostty.KKeybind:
		hs = append(hs, hint{k: "⏎", label: "keybind recorder"})
	case o.Kind == ghostty.KFont:
		hs = append(hs, hint{k: "⏎", label: "choose font"})
	case listKind(o):
		if !palette {
			hs = append(hs, hint{k: "↑↓", label: "entry"})
		}
		hs = append(hs, hint{k: "⏎", label: "edit"}, hint{k: "+", label: "add"}, hint{k: "⌫", label: "remove"})
	case o.Kind == ghostty.KFlags:
		hs = append(hs, hint{k: "←→", label: "pick"}, hint{k: "space", label: "toggle"}, hint{k: "⏎", label: "done"})
	case a.adjustable(o):
		hs = append(hs, hint{k: "←→", label: "adjust"})
		if o.Kind.Numeric() {
			fine := "fine"
			if o.Kind == ghostty.KInt {
				fine = "×10"
			}
			hs = append(hs, hint{k: "⇧←→", label: fine})
		}
		hs = append(hs, hint{k: "⏎", label: "done"}, hint{k: "⇥", label: "type"})
	default:
		hs = append(hs, hint{k: "⏎", label: "edit"})
	}
	if !listKind(o) && a.overridden(o.Key) && (!palette || a.pal.focus == focusEditor) {
		hs = append(hs, hint{k: "⌫", label: "default"})
	}
	return append(hs, esc)
}

// hintLine renders hints into one line of w cells (no status).
func (a *App) hintLine(hs []hint, w int) string {
	var left, right []string
	used := 0
	for _, h := range hs {
		if h.right {
			right = append(right, renderHint(h))
			used += width(renderHint(h)) + 3
		}
	}
	for _, h := range hs {
		if h.right {
			continue
		}
		s := renderHint(h)
		if used+width(s)+3 > w {
			break
		}
		left = append(left, s)
		used += width(s) + 3
	}
	return spread(strings.Join(left, "   "), strings.Join(right, "   "), w)
}

// editorBody renders the expanded value editor for o in w cells and at most
// maxH lines: a large value, the type's control, a preview and the docs.
func (a *App) editorBody(o *ghostty.Option, w, maxH int) []string {
	cur := a.cur(o)
	var top []string
	switch {
	case a.ed.texting && a.ed.o == o:
		in := sAccB.Render("› ") + a.ed.text.View("value", true, w-12)
		top = []string{in}
		if o.Kind == ghostty.KColor {
			if _, _, _, ok := ghostty.ParseHex(a.ed.text.Value()); ok {
				top[0] = swatch(a.ed.text.Value(), "██") + "  " + in
			}
		}
		if len(o.Values) > 0 {
			top = append(top, "", sFaint.Render("suggested  ")+sMuted.Render(trunc(strings.Join(o.Values, "  "), w-11)))
		}
	case o.Kind.Numeric():
		v, err := strconv.ParseFloat(cur, 64)
		def := sFaint.Render("default " + orUnset(o.Default()))
		if err != nil {
			top = []string{sBold.Render(orUnset(cur)) + "   " + def}
			break
		}
		num := fmtNum(o, v)
		if big := bigNumber(num, sBold); big != nil && maxH >= 9 && w >= 40 {
			top = big
			top[2] += "  " + def
		} else {
			top = []string{sBold.Render(num) + "   " + def}
		}
		if o.HasRange {
			top = append(top, "")
			top = append(top, slider(o, v, min(w-2, 40))...)
		} else {
			top = append(top, "", sFaint.Render(fmt.Sprintf("←→ ±%s   ⇧←→ ±%s   ⇥ type a value",
				fmtNum(o, o.Step(v, false)), fmtNum(o, o.Step(v, true)))))
		}
	case o.Kind == ghostty.KEnum || o.Kind == ghostty.KBool || (o.Kind == ghostty.KString && len(o.Values) > 0 && !listKind(o)):
		vals, active := o.Values, slices.Index(o.Values, cur)
		if o.Kind == ghostty.KBool {
			vals = []string{"true", "false"}
			active = slices.Index(vals, cur)
		}
		top = append(top, wrapSegments(vals, active, w, a.overridden(o.Key))...)
		top = append(top, "", sFaint.Render("default "+orUnset(o.Default())))
		if o.Kind == ghostty.KString && active < 0 && cur != "" {
			top[len(top)-1] = sText.Render("current "+cur) + "   " + top[len(top)-1]
		}
	case o.Kind == ghostty.KFlags:
		top = append(top, a.flagLines(o, w)...)
		top = append(top, "", sFaint.Render("default "+o.Default()))
	case o.Kind == ghostty.KColor:
		if cur == "" {
			top = []string{sMuted.Render("unset · the theme decides"), "", sFaint.Render("⏎ or ⇥ to type a color like #1a1b26")}
		} else {
			sw := swatch(cur, "██████")
			top = []string{sw, sw + "  " + sBold.Render(cur), sw + "  " + sFaint.Render("default "+orUnset(o.Default()))}
		}
	case o.Kind == ghostty.KTheme:
		top = []string{a.themeWidget(cur, w, sText), "", sFaint.Render("⏎ opens the theme studio")}
	case o.Kind == ghostty.KFont:
		top = []string{a.widget(o, w, false), "", sFaint.Render("⏎ chooses from ghostty +list-fonts")}
	case o.Kind == ghostty.KKeybind:
		top = []string{a.widget(o, w, false), "", sFaint.Render("⏎ opens the keybind recorder")}
	case listKind(o):
		top = a.listLines(o, w, max(3, maxH-6))
	default:
		v := sBold.Render(displayValue(o.Key, cur))
		if cur == "" {
			v = sMuted.Render("unset")
		}
		top = []string{v, "", sFaint.Render("⏎ edit · default " + orUnset(o.Default()))}
	}

	pv := a.preview(o, a.previewValue(o, cur), 26, 5)
	if pv != nil && w >= 66 {
		lw := w - 26 - 3
		top = strings.Split(columns(max(len(top), len(pv)), 3, []string{strings.Join(top, "\n"), strings.Join(pv, "\n")}, []int{lw, 26}), "\n")
	}

	out := append(top, "")
	if note := a.includeNote(o); note != "" {
		out = append(out, sAccent.Render(trunc(note, w)), "")
	}
	room := maxH - len(out)
	if room > 0 {
		for _, l := range clip(wrap(ghostty.Paragraphs(o.Docs, 2), w), room, w) {
			out = append(out, sSoft.Render(l))
		}
	}
	if len(out) > maxH {
		out = out[:maxH]
	}
	return out
}

func (a *App) previewValue(o *ghostty.Option, cur string) string {
	if a.ed.texting && a.ed.o == o {
		return a.ed.text.Value()
	}
	return cur
}

func orUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return s
}

// wrapSegments lays a segmented picker over as many lines as it needs.
func wrapSegments(opts []string, active, w int, ov bool) []string {
	var out []string
	line := ""
	for i, o := range opts {
		st := sMuted
		if i == active {
			st = sSeg
			if !ov {
				st = st.Foreground(cMuted)
			}
		}
		seg := st.Render(" " + o + " ")
		if width(line)+width(seg)+1 > w && line != "" {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += seg
	}
	return append(out, line)
}

func (a *App) flagLines(o *ghostty.Option, w int) []string {
	on, explicit := a.flagState(o)
	var out []string
	line := ""
	for i, f := range o.Flags {
		mark := "○ "
		st := sFaint
		if on[f] {
			mark, st = "● ", sMuted
			if explicit[f] {
				st = sText
			}
		}
		seg := st.Render(" " + mark + f + " ")
		if i == a.flagCur {
			seg = sSeg.Render(" " + mark + f + " ")
		}
		if width(line)+width(seg)+1 > w && line != "" {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += seg
	}
	return append(out, line)
}

func (a *App) listLines(o *ghostty.Option, w, h int) []string {
	vals := a.vals(o.Key)
	var out []string
	if len(vals) == 0 {
		out = append(out, sFaint.Render("no entries"))
	}
	top := scrollTo(0, a.ed.item, h, len(vals)+1)
	for i := top; i < len(vals) && len(out) < h; i++ {
		v := vals[i]
		mark := "  "
		st := sSoft
		if i == a.ed.item {
			mark, st = sAccent.Render("▌")+" ", sText
		}
		line := mark + st.Render(trunc(v, w-4))
		if o.Kind == ghostty.KPalette {
			if _, c, ok := strings.Cut(v, "="); ok {
				line = mark + swatch(strings.TrimSpace(c), "██") + " " + st.Render(trunc(v, w-7))
			}
		}
		if i == a.ed.item {
			line = paint(line, w, cRaised)
		}
		out = append(out, line)
	}
	if a.ed.item >= len(vals) {
		out = append(out, paint(sAccent.Render("▌")+" "+sText.Render("+ add entry"), w, cRaised))
	} else {
		out = append(out, "  "+sFaint.Render("+ add entry"))
	}
	return out
}

// --- palette ------------------------------------------------------------------

const (
	focusQuery = iota
	focusEditor
)

type palette struct {
	q     input
	res   []palItem
	cur   int
	top   int
	focus int
}

type palItem struct {
	o     *ghostty.Option
	idx   []int
	docs  bool
	score int
}

func (a *App) openPalette(q string) {
	a.pal = palette{}
	a.pal.q.Set(q)
	a.pal.refresh(a)
	a.ov = ovPalette
	a.syncEditor()
}

func (p *palette) selected() *ghostty.Option {
	if p.cur >= 0 && p.cur < len(p.res) {
		return p.res[p.cur].o
	}
	return nil
}

func (p *palette) refresh(a *App) {
	q := strings.TrimSpace(p.q.Value())
	var res []palItem
	if q == "" {
		over := a.overrides()
		for _, k := range over {
			res = append(res, palItem{o: a.option(k)})
		}
		for _, o := range a.schema.Options {
			if !slices.Contains(over, o.Key) && !o.Irrelevant() {
				res = append(res, palItem{o: o})
			}
		}
	} else {
		var keyHits, docHits []palItem
		for _, o := range a.schema.Options {
			if m, ok := fuzzy.Find(q, o.Key); ok {
				s := m.Score
				if o.Irrelevant() {
					s -= 60
				}
				if a.overridden(o.Key) {
					s += 12
				}
				keyHits = append(keyHits, palItem{o: o, idx: m.Indices, score: s})
			} else if fuzzy.Words(q, o.Docs) {
				docHits = append(docHits, palItem{o: o, docs: true, score: -boolInt(o.Irrelevant())})
			}
		}
		sort.SliceStable(keyHits, func(i, j int) bool { return keyHits[i].score > keyHits[j].score })
		if len(keyHits) > 0 {
			best := keyHits[0].score
			keyHits = slices.DeleteFunc(keyHits, func(h palItem) bool { return h.score < best/2 })
		}
		sort.SliceStable(docHits, func(i, j int) bool { return docHits[i].score > docHits[j].score })
		res = append(keyHits, docHits...)
	}
	p.res, p.cur, p.top = res, 0, 0
}

// syncEditor points the editor at the palette's selection.
func (a *App) syncEditor() {
	if sel := a.pal.selected(); sel != nil && sel != a.ed.o {
		a.beginEdit(sel)
	} else if sel == nil {
		a.ed = editor{}
	}
}

func (a *App) paletteKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.pal
	s := msg.String()
	sel := p.selected()
	if a.ed.texting {
		cmd, _ := a.editKey(msg)
		if !a.ed.texting && a.ov == ovNone {
			p.focus = focusQuery
		}
		return cmd
	}
	move := func(d int) tea.Cmd {
		p.cur = clamp(p.cur+d, 0, len(p.res)-1)
		p.focus = focusQuery
		a.syncEditor()
		return nil
	}
	switch s {
	case "esc":
		cmd := a.revertEdit()
		if p.focus == focusEditor {
			p.focus = focusQuery
			return cmd
		}
		a.ov = ovNone
		return cmd
	case "up", "ctrl+p":
		if !(p.focus == focusEditor && sel != nil && listKind(sel)) {
			return move(-1)
		}
	case "down", "ctrl+n":
		if !(p.focus == focusEditor && sel != nil && listKind(sel)) {
			return move(1)
		}
	case "pgup":
		return move(-8)
	case "pgdown":
		return move(8)
	case "enter":
		if sel == nil {
			return nil
		}
		if p.focus == focusQuery && a.adjustable(sel) && !listKind(sel) {
			p.focus = focusEditor
			return nil
		}
		cmd := a.editorEnter()
		if a.ov == ovPalette && !a.ed.texting {
			p.focus = focusEditor
		}
		return cmd
	case "left", "right", "shift+left", "shift+right", "tab":
		if sel == nil {
			return nil
		}
		p.focus = focusEditor
	}
	if sel != nil && (p.focus == focusEditor || s == "tab") {
		if cmd, ok := a.editKey(msg); ok {
			return cmd
		}
	}
	if p.q.Handle(msg, false) {
		p.focus = focusQuery
		p.refresh(a)
		a.syncEditor()
	}
	return nil
}

func (a *App) viewPalette() string {
	p := &a.pal
	w := clamp(a.w-6, 40, 88)
	if a.w < 46 {
		w = a.w
	}
	inner := w - 4

	count := fmt.Sprintf("%d matches", len(p.res))
	if strings.TrimSpace(p.q.Value()) == "" {
		count = fmt.Sprintf("%d options", len(p.res))
	}
	qline := sAccB.Render("›") + " " + p.q.View("search options and docs", p.focus == focusQuery && !a.ed.texting, inner-width(count)-4)
	rows := []string{spread(qline, sFaint.Render(count), inner), boxRule}

	avail := a.h - 4
	nres := clamp(len(p.res), 1, 7)
	edH := clamp(avail-7-nres, 0, 13)
	if edH < 5 {
		edH = 0
		nres = clamp(avail-5, 1, len(p.res))
	}
	p.top = scrollTo(p.top, p.cur, nres, len(p.res))
	if len(p.res) == 0 {
		rows = append(rows, sFaint.Render("no option matches · try words from the docs"))
	}
	for i := p.top; i < len(p.res) && i < p.top+nres; i++ {
		rows = append(rows, a.paletteRow(p.res[i], inner, i == p.cur))
	}
	if sel := p.selected(); sel != nil && edH > 0 {
		rows = append(rows, boxRule)
		rows = append(rows, a.editorBody(sel, inner, edH)...)
	}
	rows = append(rows, boxRule, a.hintLine(a.editorHints(true), inner))
	return box(rows, w, cBorder, cSurface)
}

func (a *App) paletteRow(it palItem, w int, sel bool) string {
	o := it.o
	const badgeW, valW = 10, 16
	nameW := w - 2 - badgeW - valW
	base := sSoft
	if sel {
		base = sText
	}
	name := lipgloss.StyleRunes(o.Key, it.idx, base.Foreground(cAccent).Bold(true), base)
	if it.docs {
		name = base.Render(o.Key) + sFaint.Render(" · matched in docs")
	}
	if o.Irrelevant() {
		name += sFaint.Render(" · " + map[string]string{"linux": "linux", "macos": "macOS"}[o.Platform])
	}
	val := sFaint.Render(trunc(displayValue(o.Key, a.cur(o)), valW-2))
	if a.overridden(o.Key) {
		val = sText.Render(trunc(displayValue(o.Key, a.cur(o)), valW-3)) + " " + sAccent.Render("•")
	}
	switch o.Kind {
	case ghostty.KKeybind:
		val = sFaint.Render(fmt.Sprintf("%d custom", len(a.vals(o.Key))))
	case ghostty.KPalette:
		val = sFaint.Render(fmt.Sprintf("%d set", len(a.vals(o.Key))))
	}
	marker := " "
	if sel {
		marker = sAccent.Render("▌")
	}
	line := marker + " " + pad(name, nameW) + pad(sMuted.Render(o.Label()), badgeW) + padLeft(val, valW)
	if sel {
		return paint(line, w, cRaised)
	}
	return line
}

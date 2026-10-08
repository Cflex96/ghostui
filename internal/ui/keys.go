package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Cflex96/ghostui/internal/config"
	"github.com/Cflex96/ghostui/internal/fuzzy"
	"github.com/Cflex96/ghostui/internal/ghostty"
)

const (
	ksIdle = iota
	ksListen
	ksAction
	ksConfirm
)

type keysScreen struct {
	stage        int
	trigger      string
	prefix       string // "global:" etc., kept when editing
	action       string
	editing      string // raw value of the binding being edited
	q            input
	res          []actRes
	rcur         int
	showDefaults bool
	cur, top     int
	want         string // select this binding once it shows up
}

type actRes struct {
	a    ghostty.Action
	idx  []int
	docs bool
}

type kbItem struct {
	b     ghostty.Binding
	raw   string
	def   bool
	group string
}

func (k *keysScreen) init() {}

func (k *keysScreen) enter(*App) { k.reset() }

func (k *keysScreen) reset() {
	k.stage, k.trigger, k.prefix, k.action, k.editing = ksIdle, "", "", "", ""
	k.q.Set("")
	k.rcur = 0
}

func (k *keysScreen) refresh(a *App) {
	q := strings.TrimSpace(k.q.Value())
	var keyHits, docHits []actRes
	type scored struct {
		r actRes
		s int
	}
	var hits []scored
	exact := false
	for _, act := range a.actions {
		if act.Name == q {
			exact = true
		}
		if q == "" {
			keyHits = append(keyHits, actRes{a: act})
		} else if m, ok := fuzzy.Find(q, act.Name); ok {
			hits = append(hits, scored{actRes{a: act, idx: m.Indices}, m.Score})
		} else if fuzzy.Words(q, act.Desc) {
			docHits = append(docHits, actRes{a: act, docs: true})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	for _, h := range hits {
		keyHits = append(keyHits, h.r)
	}
	k.res = append(keyHits, docHits...)
	if q != "" && !exact && !strings.Contains(q, " ") && (strings.Contains(q, ":") || len(k.res) == 0) {
		k.res = append(k.res, actRes{a: ghostty.Action{Name: q, Desc: "use as typed"}})
	}
	k.rcur = 0
}

// items are the custom keybinds (and, with D, the defaults they don't
// override), grouped.
func (a *App) keybindItems() []kbItem {
	var items []kbItem
	user := map[string]bool{}
	for _, v := range a.vals("keybind") {
		if b, ok := ghostty.ParseBinding(v); ok {
			items = append(items, kbItem{b: b, raw: v, group: ghostty.ActionGroup(b)})
			user[ghostty.NormalizeTrigger(b.Trigger)] = true
		}
	}
	if a.ks.showDefaults {
		if o := a.schema.Get("keybind"); o != nil {
			for _, v := range o.Defaults {
				if b, ok := ghostty.ParseBinding(v); ok && !user[ghostty.NormalizeTrigger(b.Trigger)] {
					items = append(items, kbItem{b: b, raw: v, def: true, group: ghostty.ActionGroup(b)})
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		gi, gj := slices.Index(ghostty.GroupOrder, items[i].group), slices.Index(ghostty.GroupOrder, items[j].group)
		if gi != gj {
			return gi < gj
		}
		return !items[i].def && items[j].def
	})
	return items
}

// triggerFor turns a key press into a Ghostty trigger like ctrl+alt+g.
func triggerFor(k tea.Key) string {
	var mods []string
	shift := k.Mod&tea.ModShift != 0
	name := ""
	switch k.Code {
	case tea.KeyEnter:
		name = "enter"
	case tea.KeyTab:
		name = "tab"
	case tea.KeyBackspace:
		name = "backspace"
	case tea.KeySpace:
		name = "space"
	case tea.KeyUp:
		name = "arrow_up"
	case tea.KeyDown:
		name = "arrow_down"
	case tea.KeyLeft:
		name = "arrow_left"
	case tea.KeyRight:
		name = "arrow_right"
	case tea.KeyPgUp:
		name = "page_up"
	case tea.KeyPgDown:
		name = "page_down"
	case tea.KeyHome:
		name = "home"
	case tea.KeyEnd:
		name = "end"
	case tea.KeyInsert:
		name = "insert"
	case tea.KeyDelete:
		name = "delete"
	default:
		switch {
		case k.Code >= tea.KeyF1 && k.Code <= tea.KeyF25:
			name = fmt.Sprintf("f%d", k.Code-tea.KeyF1+1)
		case k.Code > 0 && k.Code < unicode.MaxRune && unicode.IsPrint(k.Code):
			r := k.Code
			if k.BaseCode != 0 {
				r = k.BaseCode
			}
			if unicode.IsUpper(r) {
				r, shift = unicode.ToLower(r), true
			}
			name = string(r)
		}
	}
	if name == "" {
		return ""
	}
	if k.Mod&tea.ModSuper != 0 {
		mods = append(mods, "super")
	}
	if k.Mod&tea.ModCtrl != 0 {
		mods = append(mods, "ctrl")
	}
	if k.Mod&tea.ModAlt != 0 {
		mods = append(mods, "alt")
	}
	if shift {
		mods = append(mods, "shift")
	}
	return strings.Join(append(mods, name), "+")
}

var capGlyphs = map[string]string{
	"super": "⌘", "ctrl": "⌃", "alt": "⌥", "shift": "⇧",
	"enter": "⏎", "tab": "⇥", "backspace": "⌫", "escape": "esc", "space": "␣", "delete": "⌦",
	"arrow_up": "↑", "arrow_down": "↓", "arrow_left": "←", "arrow_right": "→",
	"page_up": "pgup", "page_down": "pgdn",
}

func capGlyph(p string) string {
	if g, ok := capGlyphs[strings.ToLower(p)]; ok {
		return g
	}
	if len([]rune(p)) == 1 {
		return strings.ToUpper(p)
	}
	return p
}

// chordCaps renders a trigger as small keycaps (sequences joined by ›).
func chordCaps(t string, accentLast bool) string {
	var steps []string
	for si, step := range strings.Split(t, ">") {
		parts := ghostty.ChordParts(step)
		var caps []string
		for i, p := range parts {
			c := keycap(capGlyph(p))
			if accentLast && i == len(parts)-1 && si == len(strings.Split(t, ">"))-1 {
				c = sKeycap.Foreground(cAccent).Render(" " + capGlyph(p) + " ")
			}
			caps = append(caps, c)
		}
		steps = append(steps, strings.Join(caps, " "))
	}
	return strings.Join(steps, sFaint.Render(" › "))
}

// bigCaps renders a chord as large keycaps with labels.
func bigCaps(t string) []string {
	parts := ghostty.ChordParts(t)
	if t == "" {
		parts = []string{"?"}
	}
	var blocks [][]string
	for i, p := range parts {
		g := capGlyph(p)
		inner := max(5, width(g)+4)
		bc := cKeyEdge
		st := sText
		if i == len(parts)-1 && t != "" {
			bc, st = cAccent, sAccent
		}
		if t == "" {
			st = sFaint
		}
		b := fg(bc)
		lp := (inner - width(g)) / 2
		blk := []string{
			b.Render("╭" + strings.Repeat("─", inner) + "╮"),
			b.Render("│") + strings.Repeat(" ", lp) + st.Bold(true).Render(g) + strings.Repeat(" ", inner-lp-width(g)) + b.Render("│"),
			b.Render("╰" + strings.Repeat("━", inner) + "╯"),
		}
		lbl := strings.ToLower(p)
		if t == "" {
			lbl = ""
		}
		l := (inner + 2 - width(lbl)) / 2
		blk = append(blk, strings.Repeat(" ", max(0, l))+sFaint.Render(lbl))
		blocks = append(blocks, blk)
	}
	out := make([]string, 4)
	for i, blk := range blocks {
		w := width(blk[0])
		for r := range out {
			if i > 0 {
				sep := "   "
				if r == 1 {
					sep = sFaint.Render(" + ")
				}
				out[r] += sep
			}
			out[r] += pad(blk[r], w)
		}
	}
	return out
}

func (a *App) keysKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := &a.ks
	s := msg.String()
	switch k.stage {
	case ksListen:
		if s == "esc" {
			k.reset()
			return nil, true
		}
		t := triggerFor(msg.Key())
		if t == "" {
			return nil, true
		}
		k.trigger = t
		if k.action != "" {
			k.stage = ksConfirm
		} else {
			k.stage = ksAction
			k.refresh(a)
		}
		return nil, true
	case ksAction:
		switch s {
		case "esc":
			k.reset()
		case "up", "ctrl+p":
			k.rcur = max(0, k.rcur-1)
		case "down", "ctrl+n":
			k.rcur = clamp(k.rcur+1, 0, len(k.res)-1)
		case "enter":
			if k.rcur < len(k.res) {
				k.action = k.res[k.rcur].a.Name
				if k.trigger == "" {
					k.stage = ksListen
				} else {
					k.stage = ksConfirm
				}
			}
		default:
			if k.q.Handle(msg, true) {
				k.refresh(a)
			}
		}
		return nil, true
	case ksConfirm:
		switch s {
		case "enter":
			return a.saveKeybind(), true
		case "r":
			k.stage = ksListen
		case "/", "a":
			k.stage = ksAction
			k.q.Set(k.action)
			k.refresh(a)
		case "esc":
			k.reset()
		}
		return nil, true
	}

	items := a.keybindItems()
	move := func(d int) { k.cur = clamp(k.cur+d, 0, len(items)-1) }
	var sel *kbItem
	if k.cur >= 0 && k.cur < len(items) {
		sel = &items[k.cur]
	}
	switch s {
	case "n":
		k.reset()
		k.stage = ksListen
	case "/":
		k.reset()
		k.stage = ksAction
		k.refresh(a)
	case "up", "ctrl+p":
		move(-1)
	case "down", "ctrl+n":
		move(1)
	case "pgup":
		move(-10)
	case "pgdown":
		move(10)
	case "home":
		move(-len(items))
	case "end":
		move(len(items))
	case "D":
		k.showDefaults = !k.showDefaults
	case "enter":
		if sel == nil {
			k.stage = ksListen
			return nil, true
		}
		k.reset()
		if !sel.def {
			k.editing = sel.raw
		}
		k.trigger = sel.b.Trigger
		for _, p := range sel.b.Prefixes {
			k.prefix += p + ":"
		}
		k.stage = ksAction
		k.q.Set(sel.b.Action)
		k.refresh(a)
	case "x", "backspace", "delete":
		if sel == nil {
			return nil, true
		}
		if sel.def {
			return tea.Batch(a.commit(config.Change{Key: "keybind", From: []string{}, To: []string{sel.b.Trigger + "=unbind"}}),
				a.flash("unbound the default "+sel.b.Trigger, stOK)), true
		}
		return tea.Batch(a.commit(config.Change{Key: "keybind", From: []string{sel.raw}, To: []string{}}),
			a.flash("unbound "+sel.b.Trigger+" · u undoes it", stOK)), true
	default:
		return nil, false
	}
	return nil, true
}

// conflict returns the custom binding that already uses the recorded
// trigger (other than the one being edited).
func (a *App) conflict() (string, bool) {
	k := &a.ks
	norm := ghostty.NormalizeTrigger(k.trigger)
	for _, v := range a.vals("keybind") {
		if b, ok := ghostty.ParseBinding(v); ok && v != k.editing && ghostty.NormalizeTrigger(b.Trigger) == norm {
			return v, true
		}
	}
	return "", false
}

func (a *App) defaultFor(trigger string) string {
	norm := ghostty.NormalizeTrigger(trigger)
	if o := a.schema.Get("keybind"); o != nil {
		for _, v := range o.Defaults {
			if b, ok := ghostty.ParseBinding(v); ok && ghostty.NormalizeTrigger(b.Trigger) == norm {
				return v
			}
		}
	}
	return ""
}

func (a *App) newBinding() string { return a.ks.prefix + a.ks.trigger + "=" + a.ks.action }

func (a *App) saveKeybind() tea.Cmd {
	k := &a.ks
	nv := a.newBinding()
	var from []string
	if k.editing != "" {
		from = append(from, k.editing)
	}
	if c, ok := a.conflict(); ok {
		from = append(from, c)
	}
	if from == nil {
		from = []string{}
	}
	k.want = nv
	k.reset()
	return tea.Batch(a.commit(config.Change{Key: "keybind", From: from, To: []string{nv}}), a.flash("keybind "+nv, stOK))
}

func (a *App) viewKeys() string {
	k := &a.ks
	right := ""
	var hints []hint
	switch k.stage {
	case ksListen:
		right = sAccent.Render("●") + " " + sMuted.Render("listening")
		hints = []hint{{k: "any", label: "record that combination"}, {k: "esc", label: "cancel", right: true}}
	case ksAction:
		hints = []hint{{k: "↑↓", label: "choose"}, {k: "⏎", label: "pick action"}, {k: "esc", label: "cancel", right: true}}
	case ksConfirm:
		verb := "add"
		if _, ok := a.conflict(); ok {
			verb = "replace"
		} else if k.editing != "" {
			verb = "update"
		}
		hints = []hint{{k: "⏎", label: verb}, {k: "r", label: "record another"}, {k: "a", label: "change action"}, {k: "esc", label: "cancel", right: true}}
	default:
		hints = []hint{{k: "n", label: "new"}, {k: "⏎", label: "edit"}, {k: "x", label: "unbind"}, {k: "/", label: "search actions"},
			{k: "D", label: "defaults"}, {k: "esc", label: "back", right: true}}
	}
	return a.frame([]string{"settings", "keys"}, right, false, hints, a.keysBody)
}

func (a *App) keysBody(w, h int) string {
	if w >= 96 {
		rw := min(48, w/2-2)
		lw := w - rw - 4
		return columns(h, 4, []string{a.recorderCard(lw, h), a.keybindList(rw, h)}, []int{lw, rw})
	}
	if a.ks.stage == ksIdle {
		return a.keybindList(w, h)
	}
	return a.recorderCard(w, h)
}

func (a *App) recorderCard(w, h int) string {
	k := &a.ks
	inner := w - 4
	title := "new keybind"
	if k.editing != "" {
		title = "edit keybind"
	}
	sub := "press n to record · / to start from an action"
	switch k.stage {
	case ksListen:
		sub = "press a combination · esc cancels"
	case ksAction:
		sub = "pick the action it runs"
	case ksConfirm:
		sub = "review the change"
	}
	rows := []string{spread(label(title), sFaint.Render(trunc(sub, inner-width(label(title))-2)), inner), ""}
	trig := k.trigger
	if k.stage == ksListen {
		trig = ""
	}
	for _, l := range bigCaps(trig) {
		rows = append(rows, lipgloss.PlaceHorizontal(inner, lipgloss.Center, l))
	}
	rows = append(rows, "")

	act := sFaint.Render("action") + " " + sAccB.Render("›") + " "
	switch {
	case k.stage == ksAction:
		act += k.q.View("type to search actions", true, inner-12)
	case k.action != "":
		act += sText.Render(k.action)
	default:
		act += sFaint.Render("—")
	}
	rows = append(rows, act)

	var diff []string
	if k.stage == ksConfirm {
		diff = a.diffBox(inner)
	}
	room := h - 2 - len(rows) - len(diff) - 1
	if k.stage == ksAction && room > 0 {
		n := min(room, 8)
		top := scrollTo(0, k.rcur, n, len(k.res))
		for i := top; i < len(k.res) && i < top+n; i++ {
			r := k.res[i]
			sel := i == k.rcur
			base, desc := sSoft, sFaint
			if sel {
				base, desc = sText, sMuted
			}
			nw := min(28, inner/2)
			name := lipgloss.StyleRunes(r.a.Name, r.idx, base.Foreground(cAccent).Bold(true), base)
			line := " " + pad(name, nw) + " " + desc.Render(trunc(r.a.Desc, inner-nw-3))
			if sel {
				line = paint(line, inner, cRaised)
			}
			rows = append(rows, line)
		}
	}
	for len(rows) < h-2-len(diff) {
		rows = append(rows, "")
	}
	rows = append(rows, diff...)
	return box(rows[:max(0, h-2)], w, cBorder, cSurface)
}

// diffBox shows the exact config line change, like the design's conflict
// card.
func (a *App) diffBox(w int) []string {
	k := &a.ks
	nv := "keybind = " + a.newBinding()
	var rows []string
	border, bgc := cBorder, cRaised
	if c, ok := a.conflict(); ok {
		border = cErr
		line := a.file.LineOf("keybind", c)
		rows = append(rows,
			spread(sErr.Render("!")+" "+sText.Render(k.trigger+" is already bound in your config"), sFaint.Render(fmt.Sprintf("line %d", line)), w-4),
			sStrike.Render("- keybind = "+c))
		if k.editing != "" {
			rows = append(rows, sStrike.Render("- keybind = "+k.editing))
		}
	} else if k.editing != "" {
		rows = append(rows, sMuted.Render("edit"), sStrike.Render("- keybind = "+k.editing))
	} else if d := a.defaultFor(k.trigger); d != "" {
		rows = append(rows, sMuted.Render("overrides the default ")+sFaint.Render(d))
	} else {
		rows = append(rows, sMuted.Render("new line in "+tilde(a.opt.Path)))
	}
	rows = append(rows, sOK.Render("+ "+nv))
	for i, r := range rows {
		rows[i] = trunc(r, w-4)
	}
	return strings.Split(box(rows, w, border, bgc), "\n")
}

func (a *App) keybindList(w, h int) string {
	k := &a.ks
	items := a.keybindItems()
	if k.want != "" {
		for i, it := range items {
			if it.raw == k.want {
				k.cur = i
			}
		}
		k.want = ""
	}
	k.cur = clamp(k.cur, 0, len(items)-1)
	custom := len(a.vals("keybind"))
	out := []string{spread(label("your keybinds"), sFaint.Render(fmt.Sprintf("%d custom", custom)), w), ""}

	type line struct {
		s    string
		item int
	}
	var ls []line
	group := ""
	for i, it := range items {
		if it.group != group {
			if group != "" {
				ls = append(ls, line{"", -1})
			}
			group = it.group
			ls = append(ls, line{sFaint.Render(group), -1})
		}
		sel := i == k.cur && k.stage == ksIdle
		marker := "  "
		act := sSoft
		if it.def {
			act = sFaint
		}
		if sel {
			marker, act = sAccent.Render("▌")+" ", sText
		}
		caps := chordCaps(it.b.Trigger, sel)
		cw := max(14, min(22, w/2-4))
		var tags []string
		if it.b.Global() {
			tags = append(tags, "global")
		}
		if it.def {
			tags = append(tags, "default")
		}
		tag := strings.Join(tags, " ")
		s := marker + pad(caps, cw) + " " + act.Render(trunc(it.b.Action, w-cw-4-width(tag)))
		if tag != "" {
			s = spread(s, sFaint.Render(tag), w)
		}
		if sel {
			s = paint(s, w, cRaised)
		}
		ls = append(ls, line{s, i})
	}
	if len(items) == 0 {
		ls = append(ls, line{sMuted.Render("No custom keybinds yet."), -1}, line{"", -1},
			line{sFaint.Render("Press ") + keycap("n") + sFaint.Render(" to record one."), -1})
	}
	listH := h - len(out) - 2
	curLine := 0
	for i, l := range ls {
		if l.item == k.cur {
			curLine = i
		}
	}
	k.top = scrollTo(k.top, curLine, listH, len(ls))
	for i := k.top; i < len(ls) && i < k.top+listH; i++ {
		out = append(out, ls[i].s)
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	toggle := "show defaults too"
	if k.showDefaults {
		toggle = "hide defaults"
	}
	out = append(out, sFaint.Render(toggle+"  ")+keycap("D"))
	return strings.Join(out, "\n")
}

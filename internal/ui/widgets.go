package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Cflex96/ghostui/internal/ghostty"
)

// widget renders o's value inline in w cells. The selected row gets the
// interactive form (pickers, ‹ › steppers) where there's room.
func (a *App) widget(o *ghostty.Option, w int, sel bool) string {
	ov := a.overridden(o.Key)
	vs := sText
	if !ov {
		vs = sFaint
	}
	cur := a.cur(o)
	switch o.Kind {
	case ghostty.KBool:
		on := cur == "true"
		if sel || ov {
			idx := 1
			if on {
				idx = 0
			} else if cur == "" {
				idx = -1
			}
			return segmented([]string{"on", "off"}, idx, w, ov)
		}
		if cur == "" {
			return vs.Render("unset")
		}
		return vs.Render(map[bool]string{true: "on", false: "off"}[on])
	case ghostty.KEnum:
		if sel || ov {
			if s := segmented(o.Values, slices.Index(o.Values, cur), w, ov); s != "" {
				return s
			}
		}
		if sel {
			return stepper(cur, vs)
		}
		return vs.Render(cur)
	case ghostty.KInt, ghostty.KFloat:
		v, err := strconv.ParseFloat(cur, 64)
		if err != nil {
			if cur == "" {
				return sFaint.Render("unset")
			}
			return vs.Render(trunc(cur, w))
		}
		s := vs.Render(fmtNum(o, v))
		if o.HasRange {
			s = bar(o, v, 10, ov) + "  " + s
		} else if sel {
			s = stepper(fmtNum(o, v), vs)
		}
		if ov && cur != o.Default() && o.Default() != "" {
			if note := "   " + sFaint.Render("default "+o.Default()); width(s+note) <= w {
				s += note
			}
		}
		return s
	case ghostty.KColor:
		if cur == "" {
			return sFaint.Render("unset · from theme")
		}
		return swatch(cur, "██") + " " + vs.Render(cur)
	case ghostty.KTheme:
		return a.themeWidget(cur, w, vs)
	case ghostty.KFont:
		l := a.list(o.Key)
		if len(l) == 0 {
			return sFaint.Render("system default")
		}
		s := vs.Render(l[0])
		if len(l) > 1 {
			s += sFaint.Render(" + " + strings.Join(l[1:], ", "))
		}
		return trunc(s, w)
	case ghostty.KFlags:
		return a.flagsWidget(o, w, sel)
	case ghostty.KPalette:
		vals := a.vals(o.Key)
		if len(vals) == 0 {
			return sFaint.Render("from theme")
		}
		var s string
		for _, v := range vals {
			if _, c, ok := strings.Cut(v, "="); ok {
				s += swatch(strings.TrimSpace(c), "█")
			}
		}
		return s + sFaint.Render(fmt.Sprintf("  %d colors", len(vals)))
	case ghostty.KKeybind:
		n := len(a.vals(o.Key))
		if n == 0 {
			return sFaint.Render("defaults only")
		}
		return vs.Render(fmt.Sprintf("%d custom", n)) + sFaint.Render("  ⏎ keys")
	}
	if o.Repeatable && len(a.vals(o.Key)) > 1 {
		return trunc(vs.Render(strings.Join(a.vals(o.Key), sFaint.Render(" · "))), w)
	}
	if cur == "" {
		return sFaint.Render("unset")
	}
	if sel && len(o.Values) > 0 {
		return stepper(displayValue(o.Key, cur), vs)
	}
	return vs.Render(trunc(displayValue(o.Key, cur), w))
}

func stepper(v string, st lipgloss.Style) string {
	return sFaint.Render("‹ ") + st.Render(v) + sFaint.Render(" ›")
}

// segmented renders an inline picker with the active option highlighted.
// It returns "" if it doesn't fit in w.
func segmented(opts []string, active, w int, ov bool) string {
	var parts []string
	for i, o := range opts {
		if i == active {
			st := sSeg
			if !ov {
				st = st.Foreground(cMuted)
			}
			parts = append(parts, st.Render(" "+o+" "))
		} else {
			parts = append(parts, sFaint.Render(" "+o+" "))
		}
	}
	s := strings.Join(parts, "")
	if width(s) > w {
		return ""
	}
	return s
}

// bar renders a ▰▰▰▱ meter of n cells for a ranged number.
func bar(o *ghostty.Option, v float64, n int, ov bool) string {
	frac := (v - o.Min) / (o.Max - o.Min)
	filled := int(math.Round(clampF(frac, 0, 1) * float64(n)))
	c := cAccent
	if !ov {
		c = cFaint
	}
	return fg(c).Render(strings.Repeat("▰", filled)) + fg(cTrack).Render(strings.Repeat("▱", n-filled))
}

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(v, hi)) }

func fmtNum(o *ghostty.Option, v float64) string {
	if o.Kind == ghostty.KInt {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64)
}

// adjustable reports whether ←/→ change o's value.
func (a *App) adjustable(o *ghostty.Option) bool {
	switch o.Kind {
	case ghostty.KBool, ghostty.KEnum, ghostty.KInt, ghostty.KFloat, ghostty.KFlags:
		return true
	case ghostty.KString:
		return len(o.Values) > 0
	}
	return false
}

// adjust steps o's value in direction dir (±1); fine is shift.
func (a *App) adjust(o *ghostty.Option, dir int, fine bool) tea.Cmd {
	cur := a.cur(o)
	switch o.Kind {
	case ghostty.KBool:
		nv := "true"
		if cur == "true" {
			nv = "false"
		}
		return a.set(o.Key, []string{nv})
	case ghostty.KEnum, ghostty.KString:
		if len(o.Values) == 0 {
			return nil
		}
		i := slices.Index(o.Values, cur)
		if i < 0 && dir < 0 {
			i = 0
		}
		i = (i + dir + len(o.Values)) % len(o.Values)
		return a.set(o.Key, []string{o.Values[i]})
	case ghostty.KInt, ghostty.KFloat:
		v, err := strconv.ParseFloat(cur, 64)
		if err != nil && cur != "" {
			// "2,4" or "10%": stepping would throw that away.
			return a.flash(o.Key+" is "+cur+", not a plain number · ⇥ in the editor types a value", stInfo)
		}
		return a.set(o.Key, []string{fmtNum(o, stepNum(o, v, dir, fine))})
	}
	return nil
}

// stepNum moves v to the next point on the step grid in direction dir, so
// an off-grid 0.92 goes to 0.95 or 0.90.
func stepNum(o *ghostty.Option, v float64, dir int, fine bool) float64 {
	step := o.Step(v, fine)
	q := v / step
	var n float64
	if dir > 0 {
		n = math.Floor(q+1e-9) + 1
	} else {
		n = math.Ceil(q-1e-9) - 1
	}
	nv := n * step
	if o.HasRange {
		nv = clampF(nv, o.Min, o.Max)
	}
	if o.Kind == ghostty.KInt && !o.HasRange && v >= 0 {
		nv = math.Max(nv, 0)
	}
	return nv
}

// flagState returns each flag's effective on/off: the defaults, overridden
// by flags the user spelled out.
func (a *App) flagState(o *ghostty.Option) (on map[string]bool, explicit map[string]bool) {
	on, explicit = map[string]bool{}, map[string]bool{}
	for _, f := range strings.Split(o.Default(), ",") {
		on[strings.TrimPrefix(f, "no-")] = !strings.HasPrefix(f, "no-")
	}
	for _, v := range a.vals(o.Key) {
		if v == "true" || v == "false" {
			for _, f := range o.Flags {
				on[f] = v == "true"
				explicit[f] = true
			}
			continue
		}
		for _, f := range strings.Split(v, ",") {
			f = strings.TrimSpace(f)
			name := strings.TrimPrefix(f, "no-")
			if name == "" {
				continue
			}
			on[name] = !strings.HasPrefix(f, "no-")
			explicit[name] = true
		}
	}
	return on, explicit
}

// toggleFlag flips one flag, writing only the flags the user set explicitly
// (Ghostty keeps defaults for the rest).
func (a *App) toggleFlag(o *ghostty.Option, i int) tea.Cmd {
	if i < 0 || i >= len(o.Flags) {
		return nil
	}
	on, explicit := a.flagState(o)
	f := o.Flags[i]
	on[f] = !on[f]
	explicit[f] = true
	var parts []string
	for _, name := range o.Flags {
		if !explicit[name] {
			continue
		}
		if on[name] {
			parts = append(parts, name)
		} else {
			parts = append(parts, "no-"+name)
		}
	}
	return a.set(o.Key, []string{strings.Join(parts, ",")})
}

func (a *App) flagsWidget(o *ghostty.Option, w int, sel bool) string {
	on, explicit := a.flagState(o)
	var parts []string
	for i, f := range o.Flags {
		st := sFaint
		if on[f] {
			st = sMuted
			if explicit[f] {
				st = sText
			}
		}
		mark := "○ "
		if on[f] {
			mark = "● "
		}
		s := st.Render(mark + f)
		if sel && i == a.flagCur && a.ov == ovNone {
			s = sSeg.Render(mark + f)
		}
		parts = append(parts, s)
	}
	return trunc(strings.Join(parts, "  "), w)
}

func (a *App) themeWidget(cur string, w int, vs lipgloss.Style) string {
	if cur == "" {
		return sFaint.Render("built-in default")
	}
	tv := ghostty.ParseThemeValue(cur)
	var strip string
	if t := a.themeBy[tv.Dark]; t != nil {
		for _, c := range t.Swatch()[:6] {
			strip += swatch(c, "█")
		}
		strip += " "
	}
	name := vs.Render(tv.Dark)
	if tv.Pair() {
		name = vs.Render(tv.Light) + sFaint.Render(" ◐ ") + vs.Render(tv.Dark)
	}
	return trunc(strip+name, w)
}

// --- large renderings for the editor ----------------------------------------

var bigGlyphs = map[rune][3]string{
	'0': {"█▀█", "█ █", "▀▀▀"},
	'1': {"▀█ ", " █ ", "▀▀▀"},
	'2': {"▀▀█", "█▀▀", "▀▀▀"},
	'3': {"▀▀█", " ▀█", "▀▀▀"},
	'4': {"█ █", "▀▀█", "  ▀"},
	'5': {"█▀▀", "▀▀█", "▀▀▀"},
	'6': {"█▀▀", "█▀█", "▀▀▀"},
	'7': {"▀▀█", "  █", "  ▀"},
	'8': {"█▀█", "█▀█", "▀▀▀"},
	'9': {"█▀█", "▀▀█", "▀▀▀"},
	'.': {" ", " ", "▀"},
	'-': {"   ", "▀▀▀", "   "},
}

// bigNumber renders s in a 3-row block font (design: large type = block
// characters), or "" if s has characters the font lacks.
func bigNumber(s string, st lipgloss.Style) []string {
	var rows [3]string
	for _, r := range s {
		g, ok := bigGlyphs[r]
		if !ok {
			return nil
		}
		for i := range rows {
			rows[i] += g[i] + " "
		}
	}
	return []string{st.Render(rows[0]), st.Render(rows[1]), st.Render(rows[2])}
}

// slider renders ━━━━●━━ with tick labels underneath.
func slider(o *ghostty.Option, v float64, w int) []string {
	frac := clampF((v-o.Min)/(o.Max-o.Min), 0, 1)
	pos := int(math.Round(frac * float64(w-1)))
	track := sAccent.Render(strings.Repeat("━", pos)) + sText.Render("●") + fg(cTrack).Render(strings.Repeat("━", w-1-pos))
	ticks := []rune(strings.Repeat(" ", w))
	for i := 0; i <= 4; i++ {
		t := o.Min + (o.Max-o.Min)*float64(i)/4
		s := strconv.FormatFloat(math.Round(t*100)/100, 'f', -1, 64)
		if o.Max-o.Min <= 1 && strings.HasPrefix(s, "0.") {
			s = s[1:]
		}
		at := int(math.Round(float64(i) / 4 * float64(w-1)))
		at = clamp(at-len(s)/2, 0, w-len(s))
		if i == 4 {
			at = w - len(s)
		}
		copy(ticks[at:], []rune(s))
	}
	return []string{track, sFaint.Render(string(ticks))}
}

// themeColors returns the background, foreground and palette the user sees:
// the active (dark) theme with background/foreground overrides applied.
func (a *App) themeColors() (bgc, fgc string, pal [16]string) {
	bgc, fgc = "#282c34", "#ffffff"
	if o := a.schema.Get("background"); o != nil && o.Default() != "" {
		bgc = o.Default()
	}
	if o := a.schema.Get("foreground"); o != nil && o.Default() != "" {
		fgc = o.Default()
	}
	for i := range pal {
		pal[i] = fgc
	}
	if t := a.themeBy[ghostty.ParseThemeValue(a.cur(a.schema.Get("theme"))).Dark]; t != nil {
		bgc, fgc, pal = t.BG, t.FG, t.Palette
	}
	if v := a.vals("background"); len(v) > 0 {
		bgc = v[len(v)-1]
	}
	if v := a.vals("foreground"); len(v) > 0 {
		fgc = v[len(v)-1]
	}
	return
}

// preview returns a small live preview for options where one helps.
func (a *App) preview(o *ghostty.Option, v string, w, h int) []string {
	switch {
	case o.Kind == ghostty.KFloat && strings.Contains(o.Key, "opacity"):
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		return a.opacityPreview(clampF(f, 0, 1), w, h)
	case o.Key == "cursor-style":
		return a.cursorPreview(v, w, h)
	case o.Kind == ghostty.KColor && v != "":
		if _, _, _, ok := ghostty.ParseHex(v); !ok {
			return nil
		}
		c := lipgloss.NewStyle().Background(lipgloss.Color(v))
		var out []string
		for i := 0; i < h; i++ {
			out = append(out, c.Render(strings.Repeat(" ", w)))
		}
		return out
	}
	return nil
}

// opacityPreview draws a mini terminal in the theme's colors over a
// checkerboard, blending the background at the given opacity.
func (a *App) opacityPreview(alpha float64, w, h int) []string {
	bgc, fgc, pal := a.themeColors()
	type seg struct{ s, c string }
	text := [][]seg{
		nil,
		{{"❯ ", pal[4]}, {"ls", fgc}},
		{{"cmd  internal", pal[6]}, {"  go.mod", fgc}},
		{{"❯ ", pal[4]}, {"█", fgc}},
	}
	var out []string
	for y := 0; y < h; y++ {
		var runes []rune
		var cols []string
		if y < len(text) {
			for _, sg := range text[y] {
				for _, r := range sg.s {
					runes, cols = append(runes, r), append(cols, sg.c)
				}
			}
		}
		var b strings.Builder
		for x := 0; x < w; x++ {
			cell := "#24242A"
			if (x/2+y)%2 == 1 {
				cell = "#34343C"
			}
			ch, col := " ", fgc
			if y > 0 && y < h-1 && x > 1 && x < w-2 {
				cell = blend(bgc, cell, alpha)
				if i := x - 3; i >= 0 && i < len(runes) {
					ch, col = string(runes[i]), cols[i]
				}
			}
			b.WriteString(lipgloss.NewStyle().Background(lipgloss.Color(cell)).Foreground(lipgloss.Color(col)).Render(ch))
		}
		out = append(out, b.String())
	}
	return out
}

func (a *App) cursorPreview(style string, w, h int) []string {
	bgc, fgc, pal := a.themeColors()
	glyph := map[string]string{"block": "█", "bar": "▏", "underline": "▁", "block_hollow": "▯"}[style]
	if glyph == "" {
		glyph = "█"
	}
	st := lipgloss.NewStyle().Background(lipgloss.Color(bgc)).Foreground(lipgloss.Color(fgc))
	acc := st.Foreground(lipgloss.Color(pal[4]))
	var out []string
	for y := 0; y < h; y++ {
		var s string
		switch y {
		case 1:
			s = acc.Render(" ❯ ") + st.Render("git status")
		case 2:
			s = acc.Render(" ❯ ") + st.Render("echo hi") + st.Render(glyph)
		}
		out = append(out, s+st.Render(strings.Repeat(" ", max(0, w-width(s)))))
	}
	return out
}

func blend(top, bottom string, alpha float64) string {
	r1, g1, b1, ok1 := ghostty.ParseHex(top)
	r2, g2, b2, ok2 := ghostty.ParseHex(bottom)
	if !ok1 || !ok2 {
		return top
	}
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*alpha + float64(y)*(1-alpha))) }
	return fmt.Sprintf("#%02x%02x%02x", mix(r1, r2), mix(g1, g2), mix(b1, b2))
}

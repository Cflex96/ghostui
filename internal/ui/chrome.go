package ui

import (
	"fmt"
	"strings"
)

type hint struct {
	k, label string
	right    bool // pinned to the right, like "esc back"
}

// margins returns the horizontal margin and inner width for the screen.
func (a *App) margins() (int, int) {
	mx := 2
	switch {
	case a.w >= 130:
		mx = 4
	case a.w >= 100:
		mx = 3
	case a.w < 60:
		mx = 1
	}
	return mx, a.w - 2*mx
}

func (a *App) compact() bool { return a.h < 28 }

// frame renders the chrome shared by every screen: header, optional tabs,
// content, and the keycap footer.
func (a *App) frame(crumbs []string, right string, showTabs bool, hints []hint, content func(w, h int) string) string {
	mx, iw := a.margins()
	compact := a.compact()
	var top []string
	if !compact {
		top = append(top, "")
	}
	top = append(top, spread(crumbLine(crumbs), right, iw))
	if !compact {
		top = append(top, "")
	}
	if showTabs {
		top = append(top, a.tabBar(iw))
		if !compact {
			top = append(top, "")
		}
	}
	foot := []string{fg(cBorder).Render(strings.Repeat("─", iw)), a.footer(hints, iw)}
	ch := max(1, a.h-len(top)-len(foot))
	all := append(top, strings.Split(fit(content(iw, ch), iw, ch), "\n")...)
	all = append(all, foot...)
	m := strings.Repeat(" ", mx)
	for i, l := range all {
		all[i] = m + l
	}
	return strings.Join(all, "\n")
}

func crumbLine(parts []string) string {
	s := sBold.Render("ghostty")
	for i, p := range parts {
		st := sMuted
		if i == len(parts)-1 && i > 0 {
			st = sText
		}
		s += sFaint.Render(" / ") + st.Render(p)
	}
	return s
}

// homeStatus is the header's right side: live preview, validity, changes.
func (a *App) homeStatus(room int) string {
	reload := sFaint.Render("○") + " " + sFaint.Render("preview off")
	if a.opt.Preview {
		reload = sOK.Render("●") + " " + sMuted.Render("live preview")
	}
	valid := sFaint.Render("… checking")
	switch {
	case !a.checked:
	case len(a.problems) == 0:
		valid = sOK.Render("✓") + " " + sMuted.Render("config valid")
	case len(a.problems) == 1:
		valid = sErr.Render("✗") + " " + sMuted.Render("1 problem")
	default:
		valid = sErr.Render("✗") + " " + sMuted.Render(fmt.Sprintf("%d problems", len(a.problems)))
	}
	changed := sAccent.Render("●") + " " + sMuted.Render(fmt.Sprintf("%d changed", len(a.overrides())))
	if n := len(a.unsaved()); n > 0 {
		changed = sAccent.Render("●") + " " + sText.Render(fmt.Sprintf("%d unsaved", n))
	}
	for _, items := range [][]string{{reload, valid, changed}, {valid, changed}, {changed}} {
		if s := strings.Join(items, "   "); width(s) <= room {
			return s
		}
	}
	return ""
}

// tabBar renders the pill tabs with underlined hotkeys, compacting on
// narrow screens.
func (a *App) tabBar(w int) string {
	hint := sFaint.Render("tab / ⇧tab")
	full := func(withHint bool) string {
		var parts []string
		for i, t := range tabs {
			if i == a.tab {
				parts = append(parts, sPill.Render(" "+t.name+" "))
				continue
			}
			parts = append(parts, " "+sUnder.Render(t.name[:1])+sMuted.Render(t.name[1:])+" ")
		}
		s := strings.Join(parts, " ")
		if withHint {
			return spread(s, hint, w)
		}
		return s
	}
	if s := full(true); width(full(false))+width(hint)+2 <= w {
		return s
	}
	if s := full(false); width(s) <= w {
		return s
	}
	// Compact: the active pill plus the other hotkeys.
	parts := []string{sPill.Render(" " + tabs[a.tab].name + " ")}
	for i, t := range tabs {
		if i != a.tab {
			parts = append(parts, sUnder.Render(t.hot))
		}
	}
	return trunc(strings.Join(parts, " "), w)
}

func (a *App) statusText() string {
	switch {
	case (a.busy || len(a.pending) > 0) && a.opt.Preview:
		return sFaint.Render("previewing…")
	case a.busy || len(a.pending) > 0:
		return sFaint.Render("checking…")
	case a.status == "":
		return ""
	case a.statusKind == stOK:
		return sOK.Render(a.status)
	case a.statusKind == stErr:
		return sErr.Render(a.status)
	}
	return sMuted.Render(a.status)
}

func renderHint(h hint) string {
	var caps []string
	for _, k := range strings.Fields(h.k) {
		caps = append(caps, keycap(k))
	}
	return strings.Join(caps, " ") + " " + sMuted.Render(h.label)
}

// footer renders keycap hints that fit, then status and pinned hints on the
// right.
func (a *App) footer(hints []hint, w int) string {
	var right []string
	if s := a.statusText(); s != "" {
		right = append(right, s)
	}
	for _, h := range hints {
		if h.right {
			right = append(right, renderHint(h))
		}
	}
	r := strings.Join(right, "   ")
	if width(r) > w*2/3 {
		r = trunc(r, w*2/3)
	}
	var left []string
	used := width(r) + 2
	for _, h := range hints {
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
	return spread(strings.Join(left, "   "), r, w)
}

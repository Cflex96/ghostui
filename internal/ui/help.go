package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"everywhere", [][2]string{
		{"/", "search every option, by name or docs"},
		{"tab ⇧tab", "next / previous tab"},
		{"o a f w k t c m", "jump to a tab"},
		{"⌃s", "save every unsaved change"},
		{"s", "review unsaved changes, revert single ones"},
		{"u  U", "undo / redo the last change"},
		{"R", "reload Ghostty's config now"},
		{"?", "this help"},
		{"q  ⌃c", "quit (asks about unsaved changes)"},
	}},
	{"lists", [][2]string{
		{"↑↓  pgup pgdn", "move"},
		{"←→  ⇧←→", "adjust the value (shift: fine)"},
		{"space", "toggle a bool or flag"},
		{"⏎", "edit, or open themes / keys / fonts"},
		{"⌫", "reset to the default"},
	}},
	{"editor and search", [][2]string{
		{"←→", "adjust; Ghostty previews it live"},
		{"⇥", "type a value"},
		{"⏎", "done"},
		{"⌫", "back to the default"},
		{"esc", "put the old value back"},
	}},
	{"theme studio", [][2]string{
		{"↑↓", "browse; the preview follows"},
		{"/", "filter by name"},
		{"⏎", "apply to the current mode"},
		{"space", "pin a theme to compare"},
		{"d  l  b", "dark · light · both"},
	}},
	{"keybinds", [][2]string{
		{"n", "record a new keybind"},
		{"⏎", "edit the selected keybind"},
		{"x", "unbind (removes the line, or unbinds a default)"},
		{"/", "pick an action first, then press keys"},
		{"D", "show the default keybinds too"},
	}},
}

func (a *App) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		a.help = max(0, a.help-1)
	case "down", "j":
		a.help++
	default:
		a.ov = ovNone
	}
	return nil
}

func (a *App) viewHelp() string {
	w := min(clamp(a.w-6, 40, 72), a.w)
	inner := w - 4
	keyW := 18
	var body []string
	for i, s := range helpSections {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, label(s.title))
		for _, k := range s.keys {
			body = append(body, sText.Render(pad(k[0], keyW))+sMuted.Render(trunc(k[1], inner-keyW)))
		}
	}
	body = append(body, "", sFaint.Render(trunc("Nothing is saved until you press ⌃s. Every change is checked", inner)),
		sFaint.Render(trunc("with ghostty +validate-config; comments, order and repeated", inner)),
		sFaint.Render(trunc("keys in your file are kept.", inner)))
	n := max(3, a.h-8)
	a.help = clamp(a.help, 0, max(0, len(body)-n))
	if len(body) > n {
		body = body[a.help : a.help+n]
	}
	rows := []string{spread(sAccB.Render("keys"), sFaint.Render("ghostui"), inner), boxRule}
	rows = append(rows, body...)
	rows = append(rows, boxRule, a.hintLine([]hint{{k: "↑↓", label: "scroll"}, {k: "esc", label: "close", right: true}}, inner))
	return strings.TrimRight(box(rows, w, cBorder, cSurface), "\n")
}

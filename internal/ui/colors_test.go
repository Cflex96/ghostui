package ui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func sgr(kind int, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;2;%d;%d;%d", kind, r>>8, g>>8, b>>8)
}

func TestColorsFollowTheTerminal(t *testing.T) {
	t.Cleanup(func() { setColors(nil, nil) })
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	v := a.View()
	if v.BackgroundColor != nil {
		t.Error("the TUI must not override the terminal's background")
	}
	if strings.Contains(v.Content, sgr(48, designBG)) {
		t.Error("the screen must not be painted with the design's background")
	}

	// A light theme: every shade comes from the terminal's colors.
	bg, fgc := color.RGBA{0xfa, 0xf4, 0xed, 0xff}, color.RGBA{0x57, 0x52, 0x79, 0xff} // Rose Pine Dawn
	a.Update(tea.BackgroundColorMsg{Color: bg})
	a.Update(tea.ForegroundColorMsg{Color: fgc})
	out := a.View().Content
	for name, want := range map[string]string{
		"text":         sgr(38, fgc),
		"selected row": sgr(48, blendColor(fgc, bg, 0.075)),
		"faint text":   sgr(38, blendColor(fgc, bg, 0.37)),
		"docs panel":   sgr(48, blendColor(fgc, bg, 0.022)),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s should use %s", name, want)
		}
	}
	if strings.Contains(out, sgr(48, designBG)) || strings.Contains(out, sgr(38, designFG)) {
		t.Error("design colors left over")
	}
	r, g, b, _ := cRaised.RGBA()
	if r>>8 < 0xe0 || g>>8 < 0xe0 || b>>8 < 0xe0 {
		t.Errorf("on a light theme the selection must stay light: %v", cRaised)
	}
}

func TestRecolorAfterReload(t *testing.T) {
	asked := 0
	old := askColors
	askColors = func() tea.Cmd { asked++; return nil }
	t.Cleanup(func() { askColors = old })

	a, _ := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	if asked != 1 {
		t.Fatalf("a previewed change reloads Ghostty, so the colors are asked again: %d", asked)
	}
	press(a, "s", "d")
	if asked != 2 {
		t.Errorf("discarding reloads too: %d", asked)
	}
	b, _ := newTestApp(t) // preview off: nothing reloads
	selectRow(t, b, "font-size")
	press(b, "right", "ctrl+s")
	if asked != 2 {
		t.Errorf("no reload, no new colors: %d", asked)
	}
}

func TestPaintLeavesTheTerminalBackground(t *testing.T) {
	if got := paint("ab\ncd", 4, lipgloss.NoColor{}); got != "ab  \ncd  " {
		t.Errorf("%q", got)
	}
	if got := paint("ab", 4, cRaised); !strings.Contains(got, sgr(48, cRaised)) {
		t.Errorf("surfaces are still painted: %q", got)
	}
}

func TestUnansweredColorsOnALightTerminal(t *testing.T) {
	for v, light := range map[string]bool{"0;15": true, "0;default;7": true, "15;0": false, "7;8": false, "": false, "x": false} {
		if lightBackground(v) != light {
			t.Errorf("COLORFGBG=%q: light %v", v, !light)
		}
	}
	t.Setenv("COLORFGBG", "0;15")
	t.Cleanup(func() { setColors(nil, nil) })
	setColors(nil, nil)
	if l := luminance(cRaised); l < 0.5 {
		t.Errorf("selected rows on a light terminal that never answered are dark (%.2f)", l)
	}
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(b>>8)) / 255
}

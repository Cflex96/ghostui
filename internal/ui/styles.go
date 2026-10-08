package ui

import (
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Colors follow the terminal's color scheme. The background is the
// terminal's own and is never painted over (so background-opacity and blur
// keep working); text is its foreground; the design's neutral tokens
// (design/DESIGN.md) are blends of the two, which is what the design's
// values are over its own #0C0C0E / #ECEAE4. The accent and the ok and error
// colors are ANSI palette entries, so they follow a theme change the moment
// Ghostty reloads. App asks the terminal for its colors at start and after
// every reload, and calls setColors with the answers.
var (
	cBG     color.Color = lipgloss.NoColor{}  // the terminal's background
	cAccent             = lipgloss.Color("4") // ANSI blue
	cOK                 = lipgloss.Color("2") // ANSI green
	cErr                = lipgloss.Color("1") // ANSI red

	cInk     color.Color // text on the accent: the background color
	cText    color.Color
	cSoft    color.Color // docs body text
	cMuted   color.Color
	cStrike  color.Color // removed lines
	cFaint   color.Color
	cKeyEdge color.Color // big keycap borders
	cTrack   color.Color // empty slider/bar cells
	cActive  color.Color // active segment of a picker
	cBorder  color.Color
	cRaised  color.Color // selected row, keycaps
	cSurface color.Color // docs panel, cards
)

// The design's own colors stand in until the terminal reports its colors,
// or a light pair when COLORFGBG says the background is light: a terminal
// that never answers mustn't get dark surfaces under dark text.
var (
	designBG color.Color = lipgloss.Color("#0C0C0E")
	designFG color.Color = lipgloss.Color("#ECEAE4")
	lightBG  color.Color = lipgloss.Color("#FAF9F6")
	lightFG  color.Color = lipgloss.Color("#1C1B19")
)

// lightBackground reads COLORFGBG ("fg;bg" or "fg;default;bg"): ANSI 7 and
// 9–15 are light backgrounds.
func lightBackground(colorfgbg string) bool {
	n, err := strconv.Atoi(colorfgbg[strings.LastIndex(colorfgbg, ";")+1:])
	return err == nil && (n == 7 || n >= 9 && n <= 15)
}

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

var (
	sText   lipgloss.Style
	sBold   lipgloss.Style
	sMuted  lipgloss.Style
	sFaint  lipgloss.Style
	sSoft   lipgloss.Style
	sAccent lipgloss.Style
	sAccB   lipgloss.Style
	sOK     lipgloss.Style
	sErr    lipgloss.Style
	sKeycap lipgloss.Style
	sPill   lipgloss.Style
	sSeg    lipgloss.Style
	sUnder  lipgloss.Style
	sStrike lipgloss.Style
)

func init() { setColors(nil, nil) }

// setColors derives every token from the terminal's background and
// foreground; nil means not known (yet).
func setColors(bg, fgc color.Color) {
	b, f := bg, fgc
	fbBG, fbFG := designBG, designFG
	if lightBackground(os.Getenv("COLORFGBG")) {
		fbBG, fbFG = lightBG, lightFG
	}
	if b == nil {
		b = fbBG
	}
	if f == nil {
		f = fbFG
	}
	mix := func(t float64) color.Color { return blendColor(f, b, t) }
	cInk = b
	cText = fgc
	if cText == nil {
		cText = lipgloss.NoColor{} // the terminal's own foreground
	}
	cSoft, cMuted, cStrike, cFaint = mix(0.77), mix(0.63), mix(0.55), mix(0.37)
	cKeyEdge, cTrack, cActive, cBorder = mix(0.21), mix(0.18), mix(0.13), mix(0.12)
	cRaised, cSurface = mix(0.075), mix(0.022)

	sText = fg(cText)
	sBold = fg(cText).Bold(true)
	sMuted = fg(cMuted)
	sFaint = fg(cFaint)
	sSoft = fg(cSoft)
	sAccent = fg(cAccent)
	sAccB = fg(cAccent).Bold(true)
	sOK = fg(cOK)
	sErr = fg(cErr)
	sKeycap = lipgloss.NewStyle().Foreground(cText).Background(cRaised)
	sPill = lipgloss.NewStyle().Foreground(cInk).Background(cAccent).Bold(true)
	sSeg = lipgloss.NewStyle().Foreground(cText).Background(cActive)
	sUnder = fg(cText).Underline(true)
	sStrike = fg(cStrike).Strikethrough(true)
}

// blendColor mixes t of top over bottom.
func blendColor(top, bottom color.Color, t float64) color.Color {
	r1, g1, b1, _ := top.RGBA()
	r2, g2, b2, _ := bottom.RGBA()
	mix := func(x, y uint32) uint8 {
		return uint8(math.Round((float64(x>>8)*t + float64(y>>8)*(1-t))))
	}
	return color.RGBA{R: mix(r1, r2), G: mix(g1, g2), B: mix(b1, b2), A: 0xff}
}

// label renders a small uppercase section header.
func label(s string) string { return sFaint.Render(strings.ToUpper(s)) }

// keycap renders a key hint like the design's .k class.
func keycap(k string) string { return sKeycap.Render(" " + k + " ") }

func swatch(hex, glyph string) string {
	if hex == "" {
		return sFaint.Render("·")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(glyph)
}

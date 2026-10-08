package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Cflex96/ghostui/internal/ghostty"
)

const sampleConfig = `#font-family = "Jetbrains-Mono-Nerd-Font"
font-family = "FiraCode Nerd Font Mono"
font-size = 16
window-padding-x = 5
macos-titlebar-style = hidden
theme = TokyoNight Night
cursor-style = block
shell-integration-features = no-cursor
title = " "
background-opacity = 1
keybind = ctrl+alt+h=goto_split:left
keybind = global:super+grave_accent=toggle_quick_terminal
not-a-real-option = 1
`

// reloads counts reload_config requests. No test ever reaches the live
// Ghostty: TestMain swaps the AppleScript reload for this counter.
var reloads atomic.Int32

// timerMsg is what a timer delivers under test: drive decides whether it
// fires.
type timerMsg struct {
	d   time.Duration
	msg tea.Msg
}

func TestMain(m *testing.M) {
	tick = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return timerMsg{d, fn(time.Now())} }
	}
	reloadGhostty = func(context.Context) error { reloads.Add(1); return nil }
	os.Exit(m.Run())
}

// drive runs cmd and whatever it leads to, the way the Bubble Tea loop would,
// and reports whether the app asked to quit. Timers shorter than a second
// (the save debounce) fire at once; longer ones (status timeouts, polling,
// the quit fallback) are dropped.
func drive(a *App, cmd tea.Cmd) (quit bool) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch m := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, m...)
		case timerMsg:
			if m.d < time.Second {
				_, next := a.Update(m.msg)
				queue = append(queue, next)
			}
		case tea.QuitMsg:
			quit = true
		default:
			_, next := a.Update(m)
			queue = append(queue, next)
		}
	}
	return quit
}

// newTestApp starts an app on a scratch copy of sampleConfig, with its own
// state directory.
func newTestApp(t testing.TB) (*App, string) {
	t.Helper()
	if _, err := ghostty.Find(); err != nil {
		t.Skip("ghostty not installed")
	}
	t.Setenv("GHOSTUI_STATE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return openApp(t, path, Options{}), path
}

// previewApp is newTestApp in preview mode, as for Ghostty's active config.
func previewApp(t testing.TB) (*App, string) {
	t.Helper()
	a, path := newTestApp(t)
	a.opt.Preview, a.opt.Reload = true, true
	return a, path
}

// openApp starts an app on path (in the current state directory) with the
// real schema, themes and fonts loaded synchronously.
func openApp(t testing.TB, path string, opt Options) *App {
	t.Helper()
	c, err := ghostty.Find()
	if err != nil {
		t.Skip("ghostty not installed")
	}
	opt.Path, opt.CLI = path, c
	a := New(opt)
	for _, cmd := range []tea.Cmd{
		func() tea.Msg { s, err := ghostty.LoadSchema(bg, c); return schemaMsg{s, err} },
		func() tea.Msg { th, err := ghostty.ListThemes(bg, c); return themesMsg{th, err} },
		func() tea.Msg { f, _ := ghostty.ListFonts(bg, c); return fontsMsg{f} },
	} {
		a.Update(cmd())
	}
	acts, _ := ghostty.ListActions(bg, c, a.schema)
	a.Update(actionsMsg{acts})
	return a
}

// press sends keys one by one and runs what each of them starts. It reports
// whether the app quit.
func press(a *App, keys ...string) (quit bool) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+alt+g":
			msg = tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl | tea.ModAlt}
		case "ctrl+alt+h":
			msg = tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl | tea.ModAlt}
		case "ctrl+c":
			msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		case "ctrl+s":
			msg = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
		case "shift+right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		_, cmd := a.Update(msg)
		quit = drive(a, cmd)
	}
	return quit
}

// checkView renders and checks the frame fills the screen without
// overflowing it.
func checkView(t *testing.T, a *App, what string) string {
	t.Helper()
	v := a.View().Content
	ls := strings.Split(v, "\n")
	if len(ls) != a.h {
		t.Errorf("%s @%dx%d: %d lines, want %d", what, a.w, a.h, len(ls), a.h)
	}
	for i, l := range ls {
		if w := ansi.StringWidth(l); w > a.w {
			t.Errorf("%s @%dx%d: line %d is %d cells wide", what, a.w, a.h, i, w)
			break
		}
	}
	return ansi.Strip(v)
}

func TestRenderEverywhere(t *testing.T) {
	a, _ := newTestApp(t)
	// Some unsaved changes, so the overlay, header and left column have
	// something to show.
	a.set("font-size", []string{"18"})
	a.set("cursor-style", []string{"bar"})
	a.set("window-padding-y", []string{"4"})
	drive(a, a.flush())
	sizes := [][2]int{{200, 60}, {140, 44}, {120, 36}, {100, 30}, {80, 24}, {64, 20}, {56, 18}, {40, 14}, {30, 10}}
	for _, sz := range sizes {
		a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for i := range tabs {
			a.setTab(i)
			checkView(t, a, tabs[i].name)
		}
		a.setTab(tabOverview)
		a.openPalette("opac")
		checkView(t, a, "palette")
		press(a, "right")
		checkView(t, a, "palette editing")
		a.ov = ovNone
		a.open(a.schema.Get("font-size"))
		checkView(t, a, "editor")
		a.ov = ovNone
		a.openFonts(a.schema.Get("font-family"))
		checkView(t, a, "fonts")
		a.ov = ovHelp
		checkView(t, a, "help")
		a.ov = ovNone
		a.openChanges(false)
		checkView(t, a, "changes")
		a.openChanges(true)
		checkView(t, a, "quit dialog")
		a.ov = ovNone
		a.setTab(tabKeys)
		press(a, "n", "ctrl+alt+g", "g", "o", "t", "o")
		checkView(t, a, "keys action")
		press(a, "enter")
		checkView(t, a, "keys confirm")
		press(a, "esc")
		a.setTab(tabThemes)
		press(a, "space", "down", "down")
		checkView(t, a, "themes compare")
		press(a, "esc")
		a.setTab(tabOverview)
	}
}

func TestTinyTerminals(t *testing.T) {
	a, _ := newTestApp(t)
	a.set("font-size", []string{"18"})
	drive(a, a.flush())
	scenes := map[string]func(){
		"palette": func() { a.openPalette("opac") },
		"editor":  func() { a.open(a.schema.Get("palette")) },
		"fonts":   func() { a.openFonts(a.schema.Get("font-family")) },
		"help":    func() { a.ov = ovHelp },
		"changes": func() { a.openChanges(true) },
		"pinned":  func() { a.setTab(tabThemes); a.th.pinned = a.th.list[min(3, len(a.th.list)-1)] },
	}
	for i := range tabs {
		scenes["tab "+tabs[i].name] = func() { a.setTab(i) }
	}
	for name, setup := range scenes {
		for w := 1; w <= 44; w++ {
			for h := 1; h <= 14; h++ {
				a.ov = ovNone
				a.setTab(tabOverview)
				a.Update(tea.WindowSizeMsg{Width: w, Height: h})
				setup()
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s @%dx%d: %v", name, w, h, r)
						}
					}()
					checkView(t, a, name)
				}()
			}
		}
	}
}

func TestOverviewShowsOverridesInFileOrder(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 44})
	out := checkView(t, a, "home")
	out = out[strings.Index(out, "YOUR OVERRIDES"):]
	prev := -1
	for _, k := range []string{"font-family", "font-size", "window-padding-x", "macos-titlebar-style", "theme", "not-a-real-option"} {
		i := strings.Index(out, k)
		if i < 0 || i < prev {
			t.Fatalf("%s missing or out of order", k)
		}
		prev = i
	}
	if !strings.Contains(out, "unknown") {
		t.Error("an unknown key should be flagged")
	}
}

func TestKeySoak(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	seq := []string{"down", "right", "left", "space", "enter", "esc", "tab", "down", "right", "enter", "esc",
		"/", "c", "u", "r", "down", "right", "enter", "esc", "esc", "t", "/", "n", "o", "enter", "esc", "esc",
		"k", "n", "ctrl+alt+g", "esc", "D", "down", "down", "enter", "esc", "?", "esc", "m", "down", "enter", "esc",
		"o", "end", "enter", "esc", "a", "home", "backspace", "u", "U"}
	for i := 0; i < 3; i++ {
		for _, k := range seq {
			press(a, k)
			checkView(t, a, "soak "+k)
		}
	}
}

func TestFontPickerSetsPrimary(t *testing.T) {
	a, path := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	pick := func(q, key string) {
		a.openFonts(a.schema.Get("font-family"))
		a.fp.q.Set(q)
		a.fp.refresh(a)
		press(a, key)
	}
	pick("menlo", "tab") // add a fallback
	pick("monaco", "enter")
	got := a.vals("font-family")
	if strings.Join(got, "|") != "Monaco|Menlo" {
		t.Fatalf("want Monaco primary, Menlo kept as fallback; got %q", got)
	}
	press(a, "ctrl+s")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "font-family = \"Monaco\"\nfont-family = \"Menlo\"\n") {
		t.Errorf("file:\n%s", b)
	}
}

func TestGhostHasSquarePixels(t *testing.T) {
	want := []string{
		"  ▄▄████▄▄  ",
		"▄██████████▄",
		"██  ████  ██",
		"████████████",
		"████████████",
		"██▀ ▀██▀ ▀██",
	}
	if got := ghostLines(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ghost:\n%s", strings.Join(got, "\n"))
	}
}

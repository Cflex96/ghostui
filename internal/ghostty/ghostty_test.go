package ghostty

import (
	"context"
	"slices"
	"testing"
)

func cli(t *testing.T) *CLI {
	t.Helper()
	c, err := Find()
	if err != nil {
		t.Skip("ghostty not installed")
	}
	return c
}

func TestLoadSchema(t *testing.T) {
	c := cli(t)
	s, err := LoadSchema(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Kind{
		"font-size":                  KFloat,
		"window-padding-x":           KInt,
		"background-opacity":         KFloat,
		"cursor-style":               KEnum,
		"macos-titlebar-style":       KEnum,
		"mouse-hide-while-typing":    KBool,
		"background":                 KColor,
		"undo-timeout":               KDuration,
		"theme":                      KTheme,
		"font-family":                KFont,
		"font-family-bold":           KFont,
		"keybind":                    KKeybind,
		"palette":                    KPalette,
		"shell-integration-features": KFlags,
		"title":                      KString,
	}
	for k, kind := range want {
		o := s.Get(k)
		if o == nil {
			t.Errorf("%s: missing", k)
			continue
		}
		if o.Kind != kind {
			t.Errorf("%s: kind %v, want %v", k, o.Kind, kind)
		}
	}
	if o := s.Get("cursor-style"); !slices.Contains(o.Values, "block_hollow") {
		t.Errorf("cursor-style values %v", o.Values)
	}
	if o := s.Get("background-opacity"); !o.HasRange || o.Min != 0 || o.Max != 1 {
		t.Errorf("background-opacity range %v %v %v", o.HasRange, o.Min, o.Max)
	}
	if o := s.Get("minimum-contrast"); !o.HasRange || o.Min != 1 || o.Max != 21 {
		t.Errorf("minimum-contrast range %v %v %v", o.HasRange, o.Min, o.Max)
	}
	if o := s.Get("font-family"); !o.Repeatable {
		t.Error("font-family should be repeatable")
	}
	if o := s.Get("font-family-bold"); o.Docs == "" {
		t.Error("font-family-bold should inherit docs")
	}
	if o := s.Get("font-size"); o.Unit != "pt" {
		t.Errorf("font-size unit %q", o.Unit)
	}
	if o := s.Get("shell-integration-features"); !slices.Contains(o.Flags, "sudo") {
		t.Errorf("flags %v", o.Flags)
	}
	t.Logf("version=%s options=%d", s.Version, len(s.Options))
}

func TestThemesAndActions(t *testing.T) {
	c := cli(t)
	ctx := context.Background()
	themes, err := ListThemes(ctx, c)
	if err != nil || len(themes) < 100 {
		t.Fatalf("themes: %d %v", len(themes), err)
	}
	var dark, light int
	for _, th := range themes {
		if th.Dark {
			dark++
		} else {
			light++
		}
		if th.BG == "" || th.Palette[4] == "" {
			t.Errorf("%s: colors not loaded", th.Name)
		}
	}
	if dark == 0 || light == 0 {
		t.Errorf("dark=%d light=%d", dark, light)
	}
	s, err := LoadSchema(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	acts, err := ListActions(ctx, c, s)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range acts {
		if a.Name == "goto_split:right" {
			found = true
			if a.Desc == "" {
				t.Error("goto_split:right has no description")
			}
		}
	}
	if !found {
		t.Error("goto_split:right not offered")
	}
	fonts, err := ListFonts(ctx, c)
	if err != nil || len(fonts) == 0 {
		t.Errorf("fonts: %v %v", fonts, err)
	}
}

func TestParseBinding(t *testing.T) {
	cases := []struct {
		in, trig, act string
		global        bool
	}{
		{"ctrl+a=new_tab", "ctrl+a", "new_tab", false},
		{"super+==increase_font_size:1", "super+=", "increase_font_size:1", false},
		{"global:super+`=toggle_quick_terminal", "super+`", "toggle_quick_terminal", true},
		{"ctrl+a>n=new_window", "ctrl+a>n", "new_window", false},
		{"ctrl+x=text:=", "ctrl+x", "text:=", false},
	}
	for _, c := range cases {
		b, ok := ParseBinding(c.in)
		if !ok || b.Trigger != c.trig || b.Action != c.act || b.Global() != c.global {
			t.Errorf("%q → %+v %v", c.in, b, ok)
		}
		if b.String() != c.in {
			t.Errorf("round trip %q → %q", c.in, b.String())
		}
	}
	if NormalizeTrigger("alt+Ctrl+G") != "ctrl+alt+g" || NormalizeTrigger("cmd+shift+,") != "super+shift+," {
		t.Error(NormalizeTrigger("alt+Ctrl+G"), NormalizeTrigger("cmd+shift+,"))
	}
	if NormalizeTrigger("super++") != "super++" {
		t.Error(NormalizeTrigger("super++"))
	}
}

func TestThemeValue(t *testing.T) {
	v := ParseThemeValue("light:Rose Pine Dawn,dark:Rose Pine")
	if v.Light != "Rose Pine Dawn" || v.Dark != "Rose Pine" || !v.Pair() {
		t.Error(v)
	}
	if ParseThemeValue("TokyoNight Night").String() != "TokyoNight Night" {
		t.Error("single")
	}
}

func TestOlderThan(t *testing.T) {
	for v, older := range map[string]bool{"Ghostty 1.2.3": true, "Ghostty 0.9.0": true, "Ghostty 1.3.0": false,
		"Ghostty 1.3.1": false, "Ghostty 1.4.0-main+abc": false, "Ghostty 2.0.0": false, "": false} {
		if OlderThan(v, 1, 3) != older {
			t.Errorf("%q: older %v", v, !older)
		}
	}
}

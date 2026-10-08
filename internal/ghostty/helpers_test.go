package ghostty

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// ActiveConfigPath must agree with Ghostty's own lookup (edit.zig): the
// first non-empty candidate wins, App Support before XDG.
func TestActiveConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	xdg := filepath.Join(home, ".config", "ghostty")
	app := filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty")
	os.MkdirAll(xdg, 0o755)
	os.MkdirAll(app, 0o755)
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if got := ActiveConfigPath(); got != filepath.Join(xdg, "config") {
		t.Errorf("nothing exists: %s", got)
	}
	write(filepath.Join(xdg, "config"), "font-size = 1\n")
	if got := ActiveConfigPath(); got != filepath.Join(xdg, "config") {
		t.Errorf("classic: %s", got)
	}
	write(filepath.Join(xdg, "config.ghostty"), "font-size = 2\n")
	if got := ActiveConfigPath(); got != filepath.Join(xdg, "config.ghostty") {
		t.Errorf("config.ghostty beats config: %s", got)
	}
	// An empty App Support file (this machine has one) is skipped.
	write(filepath.Join(app, "config.ghostty"), "")
	if got := ActiveConfigPath(); got != filepath.Join(xdg, "config.ghostty") {
		t.Errorf("empty App Support file must be skipped: %s", got)
	}
	if runtime.GOOS == "darwin" {
		write(filepath.Join(app, "config"), "font-size = 3\n")
		if got := ActiveConfigPath(); got != filepath.Join(app, "config") {
			t.Errorf("App Support beats XDG: %s", got)
		}
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "elsewhere"))
	os.Remove(filepath.Join(app, "config"))
	if got := ActiveConfigPath(); got != filepath.Join(home, "elsewhere", "ghostty", "config") {
		t.Errorf("XDG_CONFIG_HOME: %s", got)
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.WriteFile(real, []byte("x"), 0o644)
	link := filepath.Join(dir, "link")
	os.Symlink(real, link)
	other := filepath.Join(dir, "other")
	os.WriteFile(other, []byte("x"), 0o644)
	if !SameFile(real, link) {
		t.Error("a symlink is the same file")
	}
	if SameFile(real, other) {
		t.Error("equal contents aren't the same file")
	}
	if SameFile(real, filepath.Join(dir, "missing")) {
		t.Error("a missing file is never the same")
	}
}

func TestValidate(t *testing.T) {
	c := cli(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	os.WriteFile(good, []byte("font-size = 14\ntheme = Rose Pine\n"), 0o644)
	if p, err := c.Validate(context.Background(), good); err != nil || len(p) != 0 {
		t.Errorf("good config: %q %v", p, err)
	}
	bad := filepath.Join(dir, "bad dir", "config")
	os.MkdirAll(filepath.Dir(bad), 0o755)
	os.WriteFile(bad, []byte("font-size = 14\nbackground = notacolor\n"), 0o644)
	p, err := c.Validate(context.Background(), bad)
	if err != nil || len(p) != 1 || !strings.Contains(p[0], "background") || !strings.Contains(p[0], ":2:") {
		t.Errorf("bad config (path with a space): %q %v", p, err)
	}
	if v := c.Version(context.Background()); !strings.HasPrefix(v, "Ghostty ") {
		t.Errorf("version %q", v)
	}
}

func TestParseHex(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b uint8
		ok      bool
	}{
		{"#ff7a3d", 0xff, 0x7a, 0x3d, true},
		{"FF7A3D", 0xff, 0x7a, 0x3d, true},
		{"#abc", 0xaa, 0xbb, 0xcc, true},
		{"#abcd", 0, 0, 0, false},
		{"#zzzzzz", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		r, g, b, ok := ParseHex(c.in)
		if ok != c.ok || r != c.r || g != c.g || b != c.b {
			t.Errorf("ParseHex(%q) = %d %d %d %v", c.in, r, g, b, ok)
		}
	}
	if Luminance("#ffffff") < 0.99 || Luminance("#000000") != 0 || Luminance("junk") != 0 {
		t.Error("luminance")
	}
}

func TestChords(t *testing.T) {
	if p := ChordParts("super++"); !slices.Equal(p, []string{"super", "+"}) {
		t.Errorf("super++ → %q", p)
	}
	if p := ChordParts("ctrl+alt+g"); !slices.Equal(p, []string{"ctrl", "alt", "g"}) {
		t.Errorf("ctrl+alt+g → %q", p)
	}
	if !IsModifier("Cmd") || !IsModifier("opt") || IsModifier("g") {
		t.Error("IsModifier")
	}
	if NormalizeTrigger("ctrl+a>shift+N") != "ctrl+a>shift+n" {
		t.Error(NormalizeTrigger("ctrl+a>shift+N"))
	}
	for _, c := range []struct{ in, group string }{
		{"global:super+`=toggle_quick_terminal", "global"},
		{"ctrl+a=goto_split:left", "splits"},
		{"ctrl+t=new_tab", "tabs"},
		{"ctrl+n=new_window", "windows"},
		{"super+c=copy_to_clipboard", "clipboard"},
		{"super+==increase_font_size:1", "font"},
		{"super+home=scroll_to_top", "scrolling"},
		{"super+r=reload_config", "other"},
	} {
		b, _ := ParseBinding(c.in)
		if g := ActionGroup(b); g != c.group {
			t.Errorf("%s: group %s, want %s", c.in, g, c.group)
		}
	}
	for _, bad := range []string{"", "ctrl+a", "ctrl+a=", "=new_tab"} {
		if _, ok := ParseBinding(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestStep(t *testing.T) {
	cases := []struct {
		o         Option
		cur       float64
		step, alt float64
	}{
		{Option{Kind: KInt}, 5, 1, 10},
		{Option{Kind: KInt}, 250, 10, 100},
		{Option{Kind: KInt, HasRange: true, Min: 0, Max: 1000}, 500, 1, 10},
		{Option{Kind: KFloat, HasRange: true, Min: 0, Max: 1}, 0.5, 0.05, 0.01},
		{Option{Kind: KFloat, HasRange: true, Min: 1, Max: 21}, 3, 1, 0.2},
		{Option{Kind: KFloat}, 0.5, 0.05, 0.01},
		{Option{Kind: KFloat}, 14, 1, 0.5},
	}
	for _, c := range cases {
		if s := c.o.Step(c.cur, false); !near(s, c.step) {
			t.Errorf("%+v at %v: step %v, want %v", c.o, c.cur, s, c.step)
		}
		if s := c.o.Step(c.cur, true); !near(s, c.alt) {
			t.Errorf("%+v at %v: shift step %v, want %v", c.o, c.cur, s, c.alt)
		}
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestParagraphs(t *testing.T) {
	docs := "First para\ncontinues.\n\n    code block\n* bullet\n\nSecond `para`.\n\nThird."
	if got := Paragraphs(docs, 2); got != "First para continues.\n\nSecond para." {
		t.Errorf("%q", got)
	}
	if got := Paragraphs(docs, 1); got != "First para continues." {
		t.Errorf("%q", got)
	}
}

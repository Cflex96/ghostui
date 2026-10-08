package config

import (
	"slices"
	"strings"
	"testing"
)

const sample = `#font-family = "Jetbrains-Mono-Nerd-Font"
font-family = "FiraCode Nerd Font Mono"
font-size = 16
window-padding-x = 5
# Rose Pine , TokyoNight Night
theme = TokyoNight Night
title = " "
keybind = ctrl+a=new_tab
keybind = ctrl+b=new_window
background-opacity = 1
#background-blur = false
`

func TestRoundTrip(t *testing.T) {
	f := Parse([]byte(sample))
	if got := string(f.Bytes()); got != sample {
		t.Fatalf("round trip changed the file:\n%s", got)
	}
	if v := f.Values("title"); len(v) != 1 || v[0] != " " {
		t.Errorf("title = %q", v)
	}
	if v := f.Values("font-family"); len(v) != 1 || v[0] != "FiraCode Nerd Font Mono" {
		t.Errorf("font-family = %q", v)
	}
	if k := f.Keys(); strings.Join(k, ",") != "font-family,font-size,window-padding-x,theme,title,keybind,background-opacity" {
		t.Errorf("keys %v", k)
	}
}

func TestReplace(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		from, to  []string
		want, not []string
	}{
		{"set in place keeps position", "font-size", nil, []string{"14"}, []string{"window-padding-x = 5\n", "font-size = 14\nwindow"}, []string{"font-size = 16"}},
		{"quoted stays quoted", "font-family", nil, []string{"Berkeley Mono"}, []string{`font-family = "Berkeley Mono"`, `#font-family = "Jetbrains-Mono-Nerd-Font"`}, nil},
		{"unset removes the line only", "window-padding-x", nil, nil, []string{"font-size = 16\n# Rose Pine"}, []string{"window-padding-x"}},
		{"new key appended at the end", "cursor-style", nil, []string{"bar"}, []string{"#background-blur = false\ncursor-style = bar\n"}, nil},
		{"keybind replace in place", "keybind", []string{"ctrl+a=new_tab"}, []string{"ctrl+a=goto_split:right"}, []string{"keybind = ctrl+a=goto_split:right\nkeybind = ctrl+b=new_window"}, []string{"new_tab"}},
		{"keybind add after the last", "keybind", []string{}, []string{"ctrl+c=reload_config"}, []string{"keybind = ctrl+b=new_window\nkeybind = ctrl+c=reload_config\nbackground"}, nil},
		{"keybind remove one", "keybind", []string{"ctrl+b=new_window"}, []string{}, []string{"keybind = ctrl+a=new_tab\nbackground"}, []string{"new_window"}},
		{"whitespace values get quoted", "title", nil, []string{"  x  "}, []string{`title = "  x  "`}, nil},
		{"multi-value set copies quoting", "font-family", nil, []string{"A", "B"}, []string{"font-family = \"A\"\nfont-family = \"B\"\nfont-size"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := Parse([]byte(sample))
			f.Replace(c.key, c.from, c.to)
			got := string(f.Bytes())
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, n := range c.not {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q in:\n%s", n, got)
				}
			}
		})
	}
}

func TestUndoRestoresBytes(t *testing.T) {
	f := Parse([]byte(sample))
	before := f.Values("keybind")
	f.Replace("keybind", []string{"ctrl+a=new_tab"}, []string{"ctrl+a=reload_config"})
	f.Replace("keybind", f.Values("keybind"), before)
	if string(f.Bytes()) != sample {
		t.Errorf("undo of keybind replace:\n%s", f.Bytes())
	}
	f = Parse([]byte(sample))
	f.Replace("font-size", nil, []string{"18"})
	f.Replace("font-size", []string{"18"}, []string{"16"})
	if string(f.Bytes()) != sample {
		t.Errorf("undo of set:\n%s", f.Bytes())
	}
}

// Order matters for repeatable keys: the first font-family is the primary,
// the rest are fallbacks.
func TestReplaceKeepsOrder(t *testing.T) {
	f := Parse([]byte(sample))
	f.Replace("font-family", nil, []string{"JetBrains Mono", "FiraCode Nerd Font Mono"})
	if v := f.Values("font-family"); strings.Join(v, "|") != "JetBrains Mono|FiraCode Nerd Font Mono" {
		t.Fatalf("new primary must come first, got %q", v)
	}
	got := string(f.Bytes())
	if !strings.Contains(got, "#font-family = \"Jetbrains-Mono-Nerd-Font\"\nfont-family = \"JetBrains Mono\"\nfont-family = \"FiraCode Nerd Font Mono\"\nfont-size") {
		t.Errorf("unexpected layout:\n%s", got)
	}

	// Reordering an existing list (fallback promoted to primary).
	f = Parse([]byte("font-family = A\nfont-family = B\nfont-size = 1\n"))
	f.Replace("font-family", nil, []string{"B", "A"})
	if got := string(f.Bytes()); got != "font-family = B\nfont-family = A\nfont-size = 1\n" {
		t.Errorf("reorder:\n%s", got)
	}

	// Redo with full before/after lists restores the order too.
	f = Parse([]byte(sample))
	before := f.Values("font-family")
	after := []string{"JetBrains Mono", "FiraCode Nerd Font Mono"}
	f.Replace("font-family", nil, after)
	f.Replace("font-family", after, before) // undo
	if string(f.Bytes()) != sample {
		t.Errorf("undo:\n%s", f.Bytes())
	}
	f.Replace("font-family", before, after) // redo
	if v := f.Values("font-family"); strings.Join(v, "|") != strings.Join(after, "|") {
		t.Errorf("redo order %q", v)
	}
}

// Undoing a removal puts the line back where it was, not at the end.
func TestUndoRemovalRestoresPosition(t *testing.T) {
	src := "keybind = ctrl+a=new_tab\n# splits\nkeybind = ctrl+b=new_window\nkeybind = ctrl+c=close_surface\n"
	f := Parse([]byte(src))
	before := f.Values("keybind")
	f.Replace("keybind", []string{"ctrl+b=new_window"}, []string{})
	f.Replace("keybind", f.Values("keybind"), before)
	if got := string(f.Bytes()); got != src {
		t.Errorf("undo of removal:\n%s", got)
	}
}

func TestEmptyFile(t *testing.T) {
	f := Parse(nil)
	f.Replace("font-size", nil, []string{"14"})
	if got := string(f.Bytes()); got != "font-size = 14\n" {
		t.Errorf("%q", got)
	}
}

func TestParseEdgeCases(t *testing.T) {
	src := "  font-size=14  \r\n\n# theme = x\nbad line without equals\nfont-family = \"A\"\nfont-size = 16 # not a comment\nkeybind = ctrl+a==new_tab"
	f := Parse([]byte(src))
	if got := string(f.Bytes()); got != src {
		t.Fatalf("round trip without a final newline:\n%q", got)
	}
	if v := f.Values("font-size"); strings.Join(v, "|") != "14|16 # not a comment" {
		t.Errorf("font-size %q", v)
	}
	if v := f.Values("keybind"); len(v) != 1 || v[0] != "ctrl+a==new_tab" {
		t.Errorf("keybind %q", v)
	}
	if f.Lines[3].IsEntry() || f.Lines[2].IsEntry() {
		t.Error("comments and junk lines must not be entries")
	}
	if k := f.Keys(); strings.Join(k, ",") != "font-size,font-family,keybind" {
		t.Errorf("keys %v", k)
	}
	if e := f.Entries("font-size"); len(e) != 2 || e[0] != 0 || e[1] != 5 {
		t.Errorf("entries %v", e)
	}

	// Rewriting in place keeps indentation, spacing and the line ending.
	f.Replace("font-size", nil, []string{"15", "16 # not a comment"})
	if !strings.HasPrefix(string(f.Bytes()), "  font-size=15  \r\n") {
		t.Errorf("in-place rewrite:\n%q", f.Bytes())
	}
}

func TestAppendKeepsTrailingBlankLines(t *testing.T) {
	f := Parse([]byte("font-size = 1\n\n\n"))
	f.Replace("theme", nil, []string{"A"})
	if got := string(f.Bytes()); got != "font-size = 1\ntheme = A\n\n\n" {
		t.Errorf("%q", got)
	}
	f = Parse([]byte("font-size = 1"))
	f.Replace("theme", nil, []string{"A"})
	if got := string(f.Bytes()); got != "font-size = 1\ntheme = A\n" {
		t.Errorf("no final newline: %q", got)
	}
}

func TestPatchOntoEditedFile(t *testing.T) {
	// The file changed since the change was made: only the values the change
	// touched move, the rest of what's there stays.
	f := Parse([]byte("keybind = a=x\nkeybind = b=y\nkeybind = c=z\n"))
	f.Replace("keybind", []string{"a=x", "b=y"}, []string{"a=x", "b=w"})
	if got := string(f.Bytes()); got != "keybind = a=x\nkeybind = b=w\nkeybind = c=z\n" {
		t.Errorf("patch replace:\n%s", got)
	}
	// Removing a value that is already gone is a no-op.
	f.Replace("keybind", []string{"q=q"}, []string{})
	if v := f.Values("keybind"); len(v) != 3 {
		t.Errorf("patch remove of a missing value: %q", v)
	}
	// A value changed since wins: its replacement isn't added next to it.
	f = Parse([]byte("font-size = 20\n"))
	f.Replace("font-size", []string{"17"}, []string{"16"})
	if got := string(f.Bytes()); got != "font-size = 20\n" {
		t.Errorf("patch over a newer value:\n%s", got)
	}
}

func TestPatch(t *testing.T) {
	for _, c := range []struct{ cur, from, to, want []string }{
		{[]string{"a"}, nil, []string{"b"}, []string{"b"}},
		{[]string{"a", "b"}, []string{"a", "b"}, []string{"c"}, []string{"c"}},
		{[]string{"x", "b"}, []string{"a", "b"}, []string{"c", "d"}, []string{"x", "d"}}, // a→c lost to x
		{[]string{"x"}, []string{"a"}, []string{"b", "c"}, []string{"x", "c"}},           // c is an addition
		{[]string{"a", "x"}, []string{"a"}, []string{"a", "b"}, []string{"a", "x", "b"}},
		{[]string{"x", "a"}, []string{"a", "b"}, []string{"a"}, []string{"x", "a"}},
	} {
		if got := Patch(c.cur, c.from, c.to); !slices.Equal(got, c.want) {
			t.Errorf("Patch(%q, %q, %q) = %q, want %q", c.cur, c.from, c.to, got, c.want)
		}
	}
}

func TestCloneIsIndependent(t *testing.T) {
	f := Parse([]byte(sample))
	c := f.Clone()
	c.Replace("font-size", nil, []string{"99"})
	if v := f.Values("font-size"); v[0] != "16" {
		t.Errorf("clone shares lines with the original: %q", v)
	}
	if string(f.Bytes()) != sample {
		t.Error("original changed")
	}
}

func TestLineOf(t *testing.T) {
	f := Parse([]byte(sample))
	if n := f.LineOf("keybind", "ctrl+b=new_window"); n != 9 {
		t.Errorf("line %d", n)
	}
	if n := f.LineOf("keybind", "nope"); n != 0 {
		t.Errorf("missing value: %d", n)
	}
}

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{`"a b"`: "a b", `" "`: " ", `""`: "", `"`: `"`, `a"`: `a"`, `"a`: `"a`, `x`: "x"} {
		if got := Unquote(in); got != want {
			t.Errorf("Unquote(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestIntroduced(t *testing.T) {
	prev := []string{"/home/c/config:3:font-size: invalid value \"x\""}
	next := []string{"/tmp/.config-123:3:font-size: invalid value \"x\"", "/tmp/.config-123:9:theme: unknown theme"}
	if got := Introduced(prev, next); len(got) != 1 || got[0] != "theme: unknown theme" {
		t.Errorf("introduced %q", got)
	}
	if got := Introduced(nil, nil); got != nil {
		t.Errorf("none: %q", got)
	}
	if s := StripLocation("/a b/c:12:x: y"); s != "x: y" {
		t.Errorf("strip %q", s)
	}
	two := &InvalidError{Problems: []string{"a", "b", "c"}}
	if two.Error() != "a (+2 more)" {
		t.Errorf("multi-problem text %q", two.Error())
	}
}

func TestByteOrderMark(t *testing.T) {
	in := "\xef\xbb\xbffont-size = 12\ntheme = A\n"
	f := Parse([]byte(in))
	if v := f.Values("font-size"); len(v) != 1 || v[0] != "12" {
		t.Fatalf("the first key is hidden behind the BOM: %q", v)
	}
	if string(f.Bytes()) != in {
		t.Error("round trip")
	}
	f.Replace("font-size", nil, []string{"14"})
	if got := string(f.Clone().Bytes()); got != "\xef\xbb\xbffont-size = 14\ntheme = A\n" {
		t.Errorf("edit: %q", got)
	}
}

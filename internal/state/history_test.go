package state

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDir(t *testing.T) {
	t.Setenv("GHOSTUI_STATE_DIR", "/x/state")
	t.Setenv("XDG_STATE_HOME", "/x/xdg")
	if d := Dir(); d != "/x/state" {
		t.Errorf("explicit dir: %s", d)
	}
	t.Setenv("GHOSTUI_STATE_DIR", "")
	if d := Dir(); d != "/x/xdg/ghostui" {
		t.Errorf("xdg: %s", d)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/x/home")
	if d := Dir(); d != "/x/home/.local/state/ghostui" {
		t.Errorf("home: %s", d)
	}
}

func TestJournal(t *testing.T) {
	t.Setenv("GHOSTUI_STATE_DIR", "/x/state")
	a, b := Journal("/c/config"), Journal("/c/config.ghostty")
	if filepath.Dir(a) != "/x/state" || !strings.HasPrefix(filepath.Base(a), "unsaved-") || !strings.HasSuffix(a, ".json") {
		t.Errorf("journal %s", a)
	}
	if a == b || Journal("/c/config") != a {
		t.Error("one stable journal per config path")
	}
}

func open(t *testing.T) (*History, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GHOSTUI_STATE_DIR", dir)
	return Open(), filepath.Join(dir, "history.json")
}

func vals(v ...string) []string { return v }

func TestAddCoalescesBursts(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	var es []Entry
	es = Add(es, "/c", "font-size", vals("13"), vals("14"), t0)
	es = Add(es, "/c", "font-size", vals("14"), vals("15"), t0.Add(time.Second))
	es = Add(es, "/c", "font-size", vals("15"), vals("16"), t0.Add(2*time.Second))
	if len(es) != 1 {
		t.Fatalf("a burst should be one entry, got %d", len(es))
	}
	e := es[0]
	if !slices.Equal(e.Before, vals("13")) || !slices.Equal(e.After, vals("16")) || !e.Time.Equal(t0.Add(2*time.Second)) {
		t.Errorf("coalesced entry %+v", e)
	}

	// After the window, a new entry starts.
	es = Add(es, "/c", "font-size", vals("16"), vals("17"), t0.Add(10*time.Second))
	if len(es) != 2 {
		t.Fatalf("want 2 entries, got %d", len(es))
	}
	// Another key, or the same key for another file, never merges.
	es = Add(es, "/c", "theme", nil, vals("A"), t0.Add(11*time.Second))
	es = Add(es, "/other", "theme", vals("A"), vals("B"), t0.Add(12*time.Second))
	if len(es) != 4 {
		t.Fatalf("want 4 entries, got %d", len(es))
	}
	// A change that doesn't continue from the last value doesn't merge either.
	es = Add(es, "/c", "font-size", vals("99"), vals("98"), t0.Add(13*time.Second))
	if len(es) != 5 {
		t.Fatalf("want 5 entries, got %d", len(es))
	}
}

func TestAddDropsRoundTrips(t *testing.T) {
	t0 := time.Now()
	if es := Add(nil, "/c", "font-size", vals("13"), vals("13"), t0); len(es) != 0 {
		t.Fatal("a no-op change was recorded")
	}
	es := Add(nil, "/c", "font-size", vals("13"), vals("14"), t0)
	es = Add(es, "/c", "font-size", vals("14"), vals("13"), t0.Add(time.Second))
	if len(es) != 0 {
		t.Errorf("a burst that ends where it started should vanish: %+v", es)
	}
}

func TestAddDoesNotTouchItsInput(t *testing.T) {
	// The app keeps the stack at the last save to reset to on discard.
	t0 := time.Now()
	saved := Add(nil, "/c", "font-size", vals("13"), vals("14"), t0)
	snapshot := slices.Clone(saved)
	_ = Add(saved, "/c", "font-size", vals("14"), vals("15"), t0.Add(time.Second))
	if !slices.Equal(saved[0].After, snapshot[0].After) {
		t.Errorf("Add changed the slice it was given: %+v", saved)
	}
}

func TestReplace(t *testing.T) {
	h, file := open(t)
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	c := Add(nil, "/c", "font-size", vals("13"), vals("14"), t0)
	c = Add(c, "/c", "theme", nil, vals("A"), t0.Add(2*time.Minute))
	h.Replace("/c", c)
	h.Replace("/other", Add(nil, "/other", "x", nil, vals("1"), t0.Add(time.Minute)))
	if _, err := os.Stat(file + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}

	again := Open()
	if len(again.Entries) != 3 || again.Entries[1].Path != "/other" {
		t.Fatalf("entries are kept in time order across files: %+v", again.Entries)
	}
	if got := again.For("/c"); len(got) != 2 || got[0].Key != "font-size" || got[1].Key != "theme" || !got[0].Time.Equal(t0) {
		t.Errorf("for /c: %+v", got)
	}

	// Replacing one file's history leaves the others alone, even ones a
	// second instance wrote after this one loaded.
	other := Open()
	other.Replace("/third", Add(nil, "/third", "y", nil, vals("2"), t0.Add(3*time.Minute)))
	h.Replace("/c", c[:1])
	final := Open()
	if len(final.For("/c")) != 1 || len(final.For("/other")) != 1 || len(final.For("/third")) != 1 {
		t.Errorf("replace clobbered other files: %+v", final.Entries)
	}

	// A corrupt file is an empty history, and the next write repairs it.
	os.WriteFile(file, []byte("{not json"), 0o644)
	broken := Open()
	if len(broken.Entries) != 0 {
		t.Errorf("corrupt file gave %+v", broken.Entries)
	}
	broken.Replace("/c", c)
	if len(Open().Entries) != 2 {
		t.Error("history not repaired")
	}
}

func TestReplaceKeepsTheNewest(t *testing.T) {
	h, _ := open(t)
	t0 := time.Now()
	var es []Entry
	for i := range keep + 20 {
		es = Add(es, "/c", "k", vals(string(rune('a'+i%26))), vals(string(rune('a'+(i+1)%26))), t0.Add(time.Duration(i)*time.Minute))
	}
	h.Replace("/c", es)
	if len(h.Entries) != keep {
		t.Fatalf("kept %d entries, want %d", len(h.Entries), keep)
	}
	if last := h.Entries[len(h.Entries)-1]; !last.Time.Equal(t0.Add(time.Duration(keep+19) * time.Minute)) {
		t.Errorf("newest entry lost: %+v", last)
	}
}

package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var ctx = context.Background()

// nopeValidator reports every `key = nope` line as invalid, like Ghostty
// does, and counts its runs.
func nopeValidator(runs *int) Validator {
	return func(_ context.Context, p string) ([]string, error) {
		if runs != nil {
			*runs++
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var out []string
		for i, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(l, " = "); ok && v == "nope" {
				out = append(out, p+":"+strconv.Itoa(i+1)+":"+k+": invalid value \"nope\"")
			}
		}
		return out, nil
	}
}

type fixture struct {
	dir, path, journal string
	store              *Store
	d                  *Draft
}

func newFixture(t *testing.T, content string) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{dir: dir, path: filepath.Join(dir, "config"), journal: filepath.Join(dir, "state", "unsaved.json")}
	if content != "" {
		if err := os.WriteFile(f.path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	f.store = &Store{Path: f.path, Validate: nopeValidator(nil)}
	f.d = NewDraft(f.store, f.journal)
	if _, err := f.d.Load(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) disk(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f *fixture) journalExists() bool {
	_, err := os.Stat(f.journal)
	return err == nil
}

// edit applies changes to the saved sample, as the app builds its working
// copy.
func edit(src string, kv ...string) []byte {
	f := Parse([]byte(src))
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			f.Replace(kv[i], nil, nil)
		} else {
			f.Replace(kv[i], nil, []string{kv[i+1]})
		}
	}
	return f.Bytes()
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".ghostui-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestDraftSaveValidatesFirst(t *testing.T) {
	f := newFixture(t, sample)
	snap, changed, err := f.d.Save(ctx, edit(sample, "font-size", "15"))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if v := snap.File.Values("font-size"); v[0] != "15" {
		t.Error(v)
	}
	if fi, _ := os.Stat(f.path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	_, _, err = f.d.Save(ctx, edit(f.disk(t), "font-size", "nope"))
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("want InvalidError, got %v", err)
	}
	if !strings.Contains(f.disk(t), "font-size = 15") {
		t.Error("rejected change was written")
	}
	noTempFiles(t, f.dir)
}

func TestDraftFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.WriteFile(real, []byte(sample), 0o644)
	link := filepath.Join(dir, "link")
	os.Symlink(real, link)
	d := NewDraft(&Store{Path: link}, "")
	d.Load()
	if _, _, err := d.Save(ctx, edit(sample, "font-size", "12")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a file")
	}
	b, _ := os.ReadFile(real)
	if !strings.Contains(string(b), "font-size = 12") {
		t.Error("target not updated")
	}
	// A preview and its restore go through the symlink too.
	d.Show(ctx, edit(string(b), "font-size", "13"), map[string][]string{"font-size": {"13"}})
	d.Discard()
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("restore replaced the symlink")
	}
	if b2, _ := os.ReadFile(real); string(b2) != string(b) {
		t.Errorf("restore through the symlink:\n%s", b2)
	}
}

func TestDraftMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config")
	d := NewDraft(&Store{Path: path}, "")
	snap, err := d.Load()
	if err != nil || len(snap.File.Lines) != 0 || snap.Exists {
		t.Fatalf("missing file should load empty: %+v %v", snap, err)
	}
	if _, _, err := d.Save(ctx, []byte("font-size = 14\n")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "font-size = 14\n" {
		t.Errorf("created %q %v", b, err)
	}
}

func TestDraftKeepsExistingProblems(t *testing.T) {
	// A file that is already broken can still get unrelated edits: only the
	// problems a change introduces block it.
	src := "font-size = nope\ntheme = A\n"
	f := newFixture(t, src)
	if _, _, err := f.d.Show(ctx, edit(src, "theme", "B"), map[string][]string{"theme": {"B"}}); err != nil {
		t.Fatalf("an existing problem blocked an unrelated change: %v", err)
	}
	err := f.d.Check(ctx, edit(src, "theme", "B", "window-padding-x", "nope"), nil)
	var inv *InvalidError
	if !errors.As(err, &inv) || len(inv.Problems) != 1 || !strings.HasPrefix(inv.Problems[0], "window-padding-x") {
		t.Fatalf("want the new problem only, got %v", err)
	}
}

func TestDraftValidatorError(t *testing.T) {
	f := newFixture(t, sample)
	f.store.Validate = func(context.Context, string) ([]string, error) { return nil, errors.New("ghostty hung") }
	for name, err := range map[string]error{
		"check": f.d.Check(ctx, edit(sample, "font-size", "1"), nil),
		"show":  second(f.d.Show(ctx, edit(sample, "font-size", "1"), nil)),
		"save":  second(f.d.Save(ctx, edit(sample, "font-size", "1"))),
	} {
		if err == nil || err.Error() != "ghostty hung" {
			t.Errorf("%s: want the validator's error, got %v", name, err)
		}
	}
	if f.disk(t) != sample {
		t.Error("file written although validation failed to run")
	}
	noTempFiles(t, f.dir)
}

func second[A, B any](_ A, _ B, err error) error { return err }

func TestDraftValidatesTheSavedVersionOnce(t *testing.T) {
	f := newFixture(t, sample)
	runs := 0
	f.store.Validate = nopeValidator(&runs)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	f.d.Show(ctx, edit(sample, "font-size", "18"), map[string][]string{"font-size": {"18"}})
	if runs != 3 {
		t.Errorf("%d validator runs for two previews, want 3 (two previews + the saved version once)", runs)
	}
	f.d.Show(ctx, []byte(sample), nil) // back to saved: nothing to validate
	if runs != 3 {
		t.Errorf("putting the saved version back ran the validator")
	}
}

func TestDraftCheckNeverWrites(t *testing.T) {
	f := newFixture(t, sample)
	staged := map[string][]string{"font-size": {"17"}}
	if err := f.d.Check(ctx, edit(sample, "font-size", "17"), staged); err != nil {
		t.Fatal(err)
	}
	if f.disk(t) != sample {
		t.Error("Check wrote the file")
	}
	if !f.journalExists() {
		t.Error("the unsaved changes should be journaled")
	}
	if err := f.d.Check(ctx, []byte(sample), nil); err != nil || f.journalExists() {
		t.Errorf("no unsaved changes, no journal: %v", err)
	}
	if err := f.d.Check(ctx, edit(sample, "font-size", "nope"), nil); err == nil {
		t.Error("invalid value passed Check")
	}
}

func TestDraftShowAndDiscard(t *testing.T) {
	f := newFixture(t, sample)
	data := edit(sample, "font-size", "17", "cursor-style", "bar")
	_, wrote, err := f.d.Show(ctx, data, map[string][]string{"font-size": {"17"}, "cursor-style": {"bar"}})
	if err != nil || !wrote {
		t.Fatal(err, wrote)
	}
	if f.disk(t) != string(data) || !f.d.Previewing() || !f.journalExists() {
		t.Fatal("preview not on disk, or not journaled")
	}
	// Showing the same thing again doesn't touch the file.
	if _, wrote, err := f.d.Show(ctx, data, map[string][]string{"font-size": {"17"}, "cursor-style": {"bar"}}); err != nil || wrote {
		t.Errorf("same preview rewritten: %v %v", wrote, err)
	}
	snap, wrote, err := f.d.Discard()
	if err != nil || !wrote {
		t.Fatal(err, wrote)
	}
	if f.disk(t) != sample {
		t.Errorf("discard must restore the exact bytes:\n%s", f.disk(t))
	}
	if string(snap.File.Bytes()) != sample || f.d.Previewing() || f.journalExists() {
		t.Error("discard left state behind")
	}
	if fi, _ := os.Stat(f.path); fi.Mode().Perm() != 0o640 {
		t.Errorf("restore changed the mode: %v", fi.Mode().Perm())
	}
	if _, wrote, _ := f.d.Discard(); wrote {
		t.Error("discarding twice wrote again")
	}
	noTempFiles(t, f.dir)
}

func TestDraftShowSavedPutsItBack(t *testing.T) {
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	if _, wrote, err := f.d.Show(ctx, []byte(sample), nil); err != nil || !wrote {
		t.Fatal(err, wrote)
	}
	if f.disk(t) != sample || f.d.Previewing() || f.journalExists() {
		t.Error("undoing every change should leave the saved file and no journal")
	}
}

func TestDraftRefusesToOverwriteOutsideEdits(t *testing.T) {
	f := newFixture(t, sample)
	os.WriteFile(f.path, []byte(sample+"theme = Outside\n"), 0o640)
	if _, _, err := f.d.Show(ctx, edit(sample, "font-size", "17"), nil); !errors.Is(err, ErrChanged) {
		t.Errorf("show: %v", err)
	}
	if _, _, err := f.d.Save(ctx, edit(sample, "font-size", "17")); !errors.Is(err, ErrChanged) {
		t.Errorf("save: %v", err)
	}
	if !strings.Contains(f.disk(t), "Outside") {
		t.Error("the outside edit was overwritten")
	}
}

func TestDraftSaveAfterPreview(t *testing.T) {
	f := newFixture(t, sample)
	data := edit(sample, "font-size", "17")
	f.d.Show(ctx, data, map[string][]string{"font-size": {"17"}})
	fi1, _ := os.Stat(f.path)
	_, changed, err := f.d.Save(ctx, data)
	if err != nil || changed {
		t.Fatalf("saving what the preview shows changes nothing on disk: %v %v", changed, err)
	}
	if fi2, _ := os.Stat(f.path); !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Error("file rewritten")
	}
	if f.d.Previewing() || f.journalExists() {
		t.Error("saved: no preview, no journal")
	}
	if _, wrote, _ := f.d.Discard(); wrote || f.disk(t) != string(data) {
		t.Error("discard after save must keep the saved version")
	}
}

func TestDraftDiscardKeepsOutsideEdits(t *testing.T) {
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	os.WriteFile(f.path, edit(f.disk(t), "cursor-style", "bar"), 0o640) // nvim, behind the preview
	snap, _, err := f.d.Discard()
	if err != nil {
		t.Fatal(err)
	}
	want := string(edit(sample, "cursor-style", "bar"))
	if f.disk(t) != want || string(snap.File.Bytes()) != want {
		t.Errorf("want the saved version plus the outside edit:\n%s", f.disk(t))
	}
}

func TestDraftDiscardKeepsAHandEditOfThePreviewedKey(t *testing.T) {
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	os.WriteFile(f.path, edit(f.disk(t), "font-size", "20"), 0o640) // the same key, by hand
	if _, _, err := f.d.Discard(); err != nil {
		t.Fatal(err)
	}
	if got := Parse([]byte(f.disk(t))).Values("font-size"); len(got) != 1 || got[0] != "20" {
		t.Errorf("the hand edit wins over the unsaved change: %q", got)
	}
}

func TestDraftShowSavedAfterOutsideEdit(t *testing.T) {
	// Undoing every change while the file was edited behind the preview:
	// the edit has to reach the app (ErrChanged → Rebase) first.
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	edited := edit(f.disk(t), "cursor-style", "bar")
	os.WriteFile(f.path, edited, 0o640)
	if _, _, err := f.d.Show(ctx, []byte(sample), nil); !errors.Is(err, ErrChanged) {
		t.Fatalf("want ErrChanged, got %v", err)
	}
	if f.disk(t) != string(edited) {
		t.Error("nothing is written before the app has seen the edit")
	}
	snap, err := f.d.Rebase()
	if err != nil {
		t.Fatal(err)
	}
	saved := snap.File.Bytes()
	if _, wrote, err := f.d.Show(ctx, saved, nil); err != nil || !wrote {
		t.Fatal(err, wrote)
	}
	if want := string(edit(sample, "cursor-style", "bar")); f.disk(t) != want {
		t.Errorf("want the saved version plus the outside edit:\n%s", f.disk(t))
	}
}

func TestDraftRebase(t *testing.T) {
	f := newFixture(t, sample)
	staged := map[string][]string{"font-size": {"17"}}
	f.d.Show(ctx, edit(sample, "font-size", "17"), staged)
	outside := edit(f.disk(t), "cursor-style", "bar")
	os.WriteFile(f.path, outside, 0o640)
	snap, err := f.d.Rebase()
	if err != nil {
		t.Fatal(err)
	}
	want := string(edit(sample, "cursor-style", "bar"))
	if string(snap.File.Bytes()) != want {
		t.Errorf("new saved version:\n%s", snap.File.Bytes())
	}
	if !f.d.Previewing() {
		t.Error("the preview is still on disk")
	}
	// The next preview builds on the new saved version.
	if _, _, err := f.d.Show(ctx, edit(want, "font-size", "18"), map[string][]string{"font-size": {"18"}}); err != nil {
		t.Fatalf("show after rebase: %v", err)
	}
	f.d.Discard()
	if f.disk(t) != want {
		t.Errorf("discard after rebase:\n%s", f.disk(t))
	}

	// Without a preview, the file simply is the new saved version.
	g := newFixture(t, sample)
	os.WriteFile(g.path, []byte("font-size = 9\n"), 0o640)
	snap, _ = g.d.Rebase()
	if v := snap.File.Values("font-size"); v[0] != "9" {
		t.Errorf("rebase without preview: %q", v)
	}
	if _, _, err := g.d.Save(ctx, []byte("font-size = 10\n")); err != nil {
		t.Errorf("save after rebase: %v", err)
	}
}

func TestDraftRemovesAFileItCreated(t *testing.T) {
	f := newFixture(t, "")
	f.d.Show(ctx, []byte("font-size = 14\n"), map[string][]string{"font-size": {"14"}})
	if f.disk(t) != "font-size = 14\n" {
		t.Fatal("preview not written")
	}
	f.d.Discard()
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Error("the preview created the file; discarding must remove it again")
	}
}

func TestDraftCloseThenRecover(t *testing.T) {
	f := newFixture(t, sample)
	staged := map[string][]string{"font-size": {"17"}}
	f.d.Show(ctx, edit(sample, "font-size", "17"), staged)
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	if f.disk(t) != sample {
		t.Error("close must put the saved version back")
	}
	if !f.journalExists() {
		t.Error("close keeps the journal so the changes can be offered again")
	}
	if _, _, err := f.d.Show(ctx, edit(sample, "font-size", "18"), staged); !errors.Is(err, ErrClosed) {
		t.Errorf("write after close: %v", err)
	}
	if f.disk(t) != sample {
		t.Error("written after close")
	}

	next := NewDraft(f.store, f.journal)
	r, err := next.Recover()
	if err != nil || r.Repaired || r.Dropped || len(r.Staged) != 1 || r.Staged["font-size"][0] != "17" {
		t.Fatalf("recover after a clean close: %+v %v", r, err)
	}
	if f.disk(t) != sample {
		t.Error("recover must not change a clean file")
	}
}

func TestDraftRecoverRepairsACrash(t *testing.T) {
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	f.d.Show(ctx, edit(sample, "font-size", "18"), map[string][]string{"font-size": {"18"}})
	// The process dies here: no Close.
	next := NewDraft(f.store, f.journal)
	r, err := next.Recover()
	if err != nil || !r.Repaired || r.Staged["font-size"][0] != "18" {
		t.Fatalf("%+v %v", r, err)
	}
	if f.disk(t) != sample {
		t.Errorf("crash not repaired:\n%s", f.disk(t))
	}
}

func TestDraftRecoverCrashBeforeTheFileWrite(t *testing.T) {
	// The journal already names the next preview, but the process died
	// before writing it: the file still holds the previous one.
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	f.d.mu.Lock()
	f.d.staged = map[string][]string{"font-size": {"18"}}
	f.d.hashes = append(lastN(f.d.hashes, 1), hash(edit(sample, "font-size", "18")))
	f.d.writeJournal()
	f.d.mu.Unlock()

	r, err := NewDraft(f.store, f.journal).Recover()
	if err != nil || !r.Repaired {
		t.Fatalf("%+v %v", r, err)
	}
	if f.disk(t) != sample {
		t.Errorf("previous preview left on disk:\n%s", f.disk(t))
	}
}

func TestDraftRecoverLeavesAnEditedFileAlone(t *testing.T) {
	f := newFixture(t, sample)
	f.d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}})
	os.WriteFile(f.path, []byte("font-size = 30\n"), 0o640) // after the crash, someone fixed it by hand
	r, err := NewDraft(f.store, f.journal).Recover()
	if err != nil || !r.Dropped || r.Staged != nil || r.Repaired {
		t.Fatalf("%+v %v", r, err)
	}
	if f.disk(t) != "font-size = 30\n" || f.journalExists() {
		t.Error("recover must leave an edited file alone and forget the changes")
	}
}

func TestDraftRecoverIgnoresBadJournals(t *testing.T) {
	f := newFixture(t, sample)
	os.MkdirAll(filepath.Dir(f.journal), 0o755)
	for _, j := range []string{`{not json`, `{"path":"/some/other/config","staged":{"a":["1"]}}`} {
		os.WriteFile(f.journal, []byte(j), 0o600)
		r, err := NewDraft(f.store, f.journal).Recover()
		if err != nil || r.Staged != nil || r.Repaired || r.Dropped || f.journalExists() {
			t.Errorf("%s: %+v %v", j, r, err)
		}
	}
	if r, err := NewDraft(f.store, f.journal).Recover(); err != nil || r.Staged != nil {
		t.Errorf("no journal: %+v %v", r, err)
	}
	if f.disk(t) != sample {
		t.Error("file changed")
	}
}

func TestDraftNoPreviewWithoutAJournal(t *testing.T) {
	f := newFixture(t, sample)
	blocker := filepath.Join(f.dir, "blocker")
	os.WriteFile(blocker, nil, 0o644)
	d := NewDraft(f.store, filepath.Join(blocker, "unsaved.json")) // can't be created
	d.Load()
	if _, _, err := d.Show(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}}); err == nil || !strings.Contains(err.Error(), "journal") {
		t.Errorf("want a journal error, got %v", err)
	}
	if f.disk(t) != sample {
		t.Error("a preview without crash protection was written")
	}
	// Without preview, the journal is only a convenience.
	if err := d.Check(ctx, edit(sample, "font-size", "17"), map[string][]string{"font-size": {"17"}}); err != nil {
		t.Errorf("check: %v", err)
	}
}

func TestStoreIncludes(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir()) // /var → /private/var on macOS
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	inner := write("sub/inner", "theme = Inner\nconfig-file = ../config\n") // a cycle back to the top
	write("colors", "theme = Colors\nkeybind = a=b\nconfig-file = \""+inner+"\"\n")
	path := write("config", "font-size = 12\nconfig-file = colors\nconfig-file = ?missing\n")
	s := &Store{Path: path}
	b, _ := os.ReadFile(path)
	inc := s.Includes(Parse(b))
	if got := inc["theme"]; got.Path != inner || !slices.Equal(got.Values, []string{"Inner"}) {
		t.Errorf("theme: %+v", got)
	}
	if got := inc["keybind"]; got.Path != filepath.Join(dir, "colors") {
		t.Errorf("keybind: %+v", got)
	}
	if _, ok := inc["font-size"]; ok {
		t.Error("the file itself isn't an include")
	}
	if len(s.Includes(Parse([]byte("font-size = 1\n")))) != 0 {
		t.Error("no config-file, no includes")
	}
}

func TestDraftReadOnlyFolder(t *testing.T) {
	f := newFixture(t, sample)
	os.Chmod(f.dir, 0o555)
	t.Cleanup(func() { os.Chmod(f.dir, 0o755) })
	err := f.d.Check(ctx, edit(sample, "font-size", "17"), nil)
	if err == nil || !strings.Contains(err.Error(), "can't write to ") || !strings.Contains(err.Error(), filepath.Base(f.dir)) {
		t.Errorf("want a read-only explanation, got %v", err)
	}
}

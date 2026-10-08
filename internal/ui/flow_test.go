package ui

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Cflex96/ghostui/internal/config"
	"github.com/Cflex96/ghostui/internal/ghostty"
	"github.com/Cflex96/ghostui/internal/state"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// writeOutside edits the file the way another program would.
func writeOutside(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	os.Chtimes(path, later, later)
}

// work is the working copy: what a save would write.
func work(a *App) string { return string(a.working().Bytes()) }

// selectRow puts the cursor of the current tab on key's row.
func selectRow(t *testing.T, a *App, key string) *ghostty.Option {
	t.Helper()
	for i, r := range a.rows() {
		if r.o != nil && r.o.Key == key {
			a.cursor[a.tab] = i
			return r.o
		}
	}
	t.Fatalf("%s is not on the %s tab", key, tabs[a.tab].name)
	return nil
}

func typeText(a *App, s string) {
	for _, r := range s {
		press(a, string(r))
	}
}

func plain(s string) string { return ansi.Strip(s) }

func journal(a *App) bool {
	_, err := os.Stat(state.Journal(a.opt.Path))
	return err == nil
}

func TestEditStaysUnsavedUntilSave(t *testing.T) {
	a, path := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 44})
	selectRow(t, a, "font-size")
	press(a, "right")
	if got := readFile(t, path); got != sampleConfig {
		t.Fatalf("an unsaved edit reached the file:\n%s", got)
	}
	if v := a.vals("font-size"); len(v) != 1 || v[0] != "17" {
		t.Errorf("the edit should show in the app: %q", v)
	}
	if u := a.unsaved(); !slices.Equal(u, []string{"font-size"}) {
		t.Errorf("unsaved %q", u)
	}
	if a.status != "not saved yet · ⌃s saves" {
		t.Errorf("status %q", a.status)
	}
	out := checkView(t, a, "dirty home")
	for _, want := range []string{"1 unsaved", "UNSAVED", "font-size 16 → 17", "⌃s save"} {
		if !strings.Contains(out, want) {
			t.Errorf("home should show %q", want)
		}
	}
	if !strings.HasPrefix(a.View().WindowTitle, "● ") {
		t.Error("the window title should mark unsaved changes")
	}

	press(a, "ctrl+s")
	if got := readFile(t, path); !strings.Contains(got, "\nfont-size = 17\n") {
		t.Fatalf("not saved:\n%s", got)
	}
	if a.status != "saved 1 change" || a.dirty() || journal(a) {
		t.Errorf("after save: status %q, dirty %v, journal %v", a.status, a.dirty(), journal(a))
	}
	h := state.Open().For(path)
	if len(h) != 1 || h[0].Key != "font-size" || !slices.Equal(h[0].Before, []string{"16"}) || !slices.Equal(h[0].After, []string{"17"}) {
		t.Errorf("history %+v", h)
	}
	out = checkView(t, a, "clean home")
	if !strings.Contains(out, "RECENT") || strings.Contains(out, "UNSAVED") {
		t.Error("a clean home shows the recent changes")
	}
	press(a, "ctrl+s")
	if a.status != "nothing to save" {
		t.Errorf("status %q", a.status)
	}
}

func TestPreviewThenDiscard(t *testing.T) {
	a, path := previewApp(t)
	reloads.Store(0)
	selectRow(t, a, "font-size")
	press(a, "right")
	if got := readFile(t, path); !strings.Contains(got, "\nfont-size = 17\n") {
		t.Fatalf("the preview should be on disk:\n%s", got)
	}
	if reloads.Load() != 1 || a.status != "previewing · ⌃s saves" {
		t.Errorf("reloads %d, status %q", reloads.Load(), a.status)
	}
	if !journal(a) {
		t.Error("a preview on disk needs a journal")
	}
	press(a, "s")
	if a.ov != ovChanges || a.chg.quit {
		t.Fatal("s opens the review overlay")
	}
	press(a, "d")
	if got := readFile(t, path); got != sampleConfig {
		t.Errorf("discard must put the exact bytes back:\n%s", got)
	}
	if reloads.Load() != 2 || a.status != "discarded 1 change · reloaded" {
		t.Errorf("reloads %d, status %q", reloads.Load(), a.status)
	}
	if a.dirty() || journal(a) || a.ov != ovNone {
		t.Error("discard left something behind")
	}
}

func TestPreviewThenSave(t *testing.T) {
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	reloads.Store(0)
	press(a, "ctrl+s")
	if got := readFile(t, path); !strings.Contains(got, "\nfont-size = 17\n") {
		t.Fatalf("file:\n%s", got)
	}
	if reloads.Load() != 0 {
		t.Error("Ghostty already shows the saved content; no reload needed")
	}
	if a.status != "saved 1 change" || journal(a) {
		t.Errorf("status %q, journal %v", a.status, journal(a))
	}
	// Quitting now leaves the saved file alone.
	if !press(a, "q") {
		t.Fatal("q quits when nothing is unsaved")
	}
	if !strings.Contains(readFile(t, path), "\nfont-size = 17\n") {
		t.Error("quitting after a save lost it")
	}
}

func TestDebounceMakesOnePreview(t *testing.T) {
	a, path := previewApp(t)
	reloads.Store(0)
	c1 := a.set("font-size", []string{"17"})
	c2 := a.set("font-size", []string{"18"})
	drive(a, tea.Batch(c1, c2))
	if n := reloads.Load(); n != 1 {
		t.Errorf("%d previews for one burst, want 1", n)
	}
	if !strings.Contains(readFile(t, path), "font-size = 18\n") {
		t.Error("the last value isn't previewed")
	}
	if len(a.steps) != 1 || !slices.Equal(a.steps[0].Before, []string{"16"}) || !slices.Equal(a.steps[0].After, []string{"18"}) {
		t.Errorf("one undo step for the burst: %+v", a.steps)
	}
}

func TestInvalidValueIsRejected(t *testing.T) {
	for _, preview := range []bool{false, true} {
		a, path := newTestApp(t)
		a.opt.Preview = preview
		a.open(a.schema.Get("background"))
		if a.ov != ovEditor || !a.ed.texting {
			t.Fatal("a color opens as a text field")
		}
		typeText(a, "notacolor")
		press(a, "enter")
		if got := readFile(t, path); got != sampleConfig {
			t.Errorf("preview=%v: an invalid value reached the file:\n%s", preview, got)
		}
		if v := a.vals("background"); len(v) != 0 || a.dirty() {
			t.Errorf("preview=%v: rejected value still shown: %q", preview, v)
		}
		if a.statusKind != stErr || !strings.Contains(a.status, "not applied") || !strings.Contains(a.status, "background") {
			t.Errorf("preview=%v: status %q", preview, a.status)
		}
		// The problem already in the file (not-a-real-option) never blocks
		// other edits.
		selectRow(t, a, "font-size")
		press(a, "right")
		if v := a.vals("font-size"); v[0] != "17" {
			t.Errorf("preview=%v: an existing problem blocked a valid change", preview)
		}
	}
}

func TestUndoRedo(t *testing.T) {
	a, path := newTestApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	press(a, "u")
	if v := a.vals("font-size"); v[0] != "16" || a.dirty() {
		t.Fatalf("undo: %q dirty=%v", v, a.dirty())
	}
	if a.status != "undid font-size" {
		t.Errorf("status %q", a.status)
	}
	press(a, "U")
	if v := a.vals("font-size"); v[0] != "17" {
		t.Errorf("redo: %q", v)
	}
	press(a, "U")
	if a.status != "nothing to redo" {
		t.Errorf("status %q", a.status)
	}
	press(a, "u", "u")
	if a.status != "nothing to undo" || a.dirty() {
		t.Errorf("status %q", a.status)
	}
	if got := readFile(t, path); got != sampleConfig {
		t.Errorf("undo and redo never write:\n%s", got)
	}
	// A new edit drops the redo stack.
	press(a, "right", "u")
	a.set("cursor-style", []string{"bar"})
	press(a, "U")
	if a.status != "nothing to redo" {
		t.Errorf("redo after a new edit: %q", a.status)
	}
}

func TestUndoReachesSavedChanges(t *testing.T) {
	a, path := newTestApp(t)
	selectRow(t, a, "font-size")
	press(a, "right", "ctrl+s")

	// The next session can undo it: that stages the old value.
	b := openApp(t, path, Options{})
	press(b, "u")
	if v := b.vals("font-size"); v[0] != "16" || !b.dirty() {
		t.Fatalf("undoing a saved change: %q dirty=%v", v, b.dirty())
	}
	if !strings.Contains(readFile(t, path), "font-size = 17\n") {
		t.Error("undo wrote the file")
	}
	// Discarding brings the change back to the undo stack.
	press(b, "s", "d")
	if len(b.steps) != 1 {
		t.Errorf("discard should restore the undo stack: %+v", b.steps)
	}
	press(b, "u", "ctrl+s")
	if got := readFile(t, path); got != sampleConfig {
		t.Errorf("saving the undo:\n%s", got)
	}
	if h := state.Open().For(path); len(h) != 0 {
		t.Errorf("an undone change leaves the history: %+v", h)
	}
}

func TestUndoWaitsForPendingEdit(t *testing.T) {
	a, path := newTestApp(t)
	a.set("font-size", []string{"20"}) // debounce not fired yet
	press(a, "u")
	if v := a.vals("font-size"); v[0] != "16" || a.dirty() {
		t.Errorf("undo should check the pending edit, then undo it: %q", v)
	}
	if readFile(t, path) != sampleConfig {
		t.Error("file written")
	}
}

func TestResetRemovesLineOnSave(t *testing.T) {
	a, path := newTestApp(t)
	selectRow(t, a, "window-padding-x")
	press(a, "backspace")
	if readFile(t, path) != sampleConfig {
		t.Fatal("reset wrote the file before saving")
	}
	press(a, "ctrl+s")
	if got := readFile(t, path); strings.Contains(got, "window-padding-x") || !strings.Contains(got, "font-size = 16\nmacos-titlebar-style") {
		t.Errorf("file:\n%s", got)
	}
}

func TestEditorEscPutsValueBack(t *testing.T) {
	a, path := previewApp(t)
	a.open(a.schema.Get("font-size"))
	press(a, "right", "right")
	if !strings.Contains(readFile(t, path), "font-size = 18\n") {
		t.Fatal("adjustments preview live")
	}
	press(a, "esc")
	if a.ov != ovNone {
		t.Error("esc closes the editor")
	}
	if got := readFile(t, path); got != sampleConfig || a.dirty() || journal(a) {
		t.Errorf("esc should put 16 back and end the preview:\n%s", got)
	}
}

func TestToggleFlagSetsOnlyExplicitFlags(t *testing.T) {
	a, _ := newTestApp(t)
	o := selectRow(t, a, "shell-integration-features")
	a.flagCur = slices.Index(o.Flags, "sudo")
	press(a, "space")
	if got := work(a); !strings.Contains(got, "shell-integration-features = no-cursor,sudo\n") {
		t.Errorf("working copy:\n%s", got)
	}
}

func TestKeybindEdits(t *testing.T) {
	a, path := newTestApp(t)
	a.setTab(tabKeys)

	// New binding.
	press(a, "n", "ctrl+alt+g")
	typeText(a, "new_window")
	press(a, "enter", "enter")
	if got := work(a); !strings.Contains(got, "keybind = global:super+grave_accent=toggle_quick_terminal\nkeybind = ctrl+alt+g=new_window\n") {
		t.Fatalf("new keybind:\n%s", got)
	}

	// Recording a trigger that's taken replaces that line in place.
	press(a, "n", "ctrl+alt+h")
	typeText(a, "new_tab")
	press(a, "enter")
	if _, ok := a.conflict(); !ok {
		t.Error("conflict not detected")
	}
	press(a, "enter")
	got := work(a)
	if !strings.Contains(got, "background-opacity = 1\nkeybind = ctrl+alt+h=new_tab\nkeybind = global:") || strings.Contains(got, "goto_split:left") {
		t.Fatalf("replace:\n%s", got)
	}

	pick := func(match func(kbItem) bool) {
		t.Helper()
		for i, it := range a.keybindItems() {
			if match(it) {
				a.ks.cur = i
				return
			}
		}
		t.Fatal("binding not listed")
	}
	// Unbinding a custom binding removes its line.
	pick(func(it kbItem) bool { return it.raw == "ctrl+alt+g=new_window" })
	press(a, "x")
	if strings.Contains(work(a), "new_window") {
		t.Error("unbind custom")
	}
	// Unbinding a default adds an explicit unbind.
	press(a, "D")
	var trig string
	pick(func(it kbItem) bool {
		if it.def {
			trig = it.b.Trigger
		}
		return it.def
	})
	press(a, "x")
	if !strings.Contains(work(a), "keybind = "+trig+"=unbind\n") {
		t.Errorf("unbind default %s:\n%s", trig, work(a))
	}

	if readFile(t, path) != sampleConfig {
		t.Error("keybind edits wrote the file before saving")
	}
	want := work(a)
	press(a, "ctrl+s")
	if got := readFile(t, path); got != want {
		t.Errorf("saved file differs from the working copy:\n%s", got)
	}
}

func TestThemeModes(t *testing.T) {
	a, _ := newTestApp(t)
	a.setTab(tabThemes)
	if a.th.mode != modeBoth {
		t.Fatal("a single theme opens in both mode")
	}
	pick := func(dark bool, not string) string {
		t.Helper()
		for i, th := range a.th.list {
			if th.Dark == dark && th.Name != not {
				a.th.cur = i
				return th.Name
			}
		}
		t.Fatal("no theme to pick")
		return ""
	}
	both := pick(true, "TokyoNight Night")
	press(a, "enter")
	if !strings.Contains(work(a), "theme = "+both+"\n") {
		t.Fatalf("both:\n%s", work(a))
	}
	if tag := a.previewTag(a.th.selected()); tag != "● in use · unsaved" {
		t.Errorf("tag %q", tag)
	}
	press(a, "enter")
	if a.status != "already in use" {
		t.Errorf("re-applying: %q", a.status)
	}
	press(a, "d")
	dark := pick(true, both)
	press(a, "enter")
	if !strings.Contains(work(a), "theme = light:"+both+",dark:"+dark+"\n") {
		t.Fatalf("dark:\n%s", work(a))
	}
	press(a, "l")
	light := pick(false, "")
	press(a, "enter")
	if !strings.Contains(work(a), "theme = light:"+light+",dark:"+dark+"\n") {
		t.Errorf("light:\n%s", work(a))
	}
}

func TestQuitAsksAboutUnsavedChanges(t *testing.T) {
	a, _ := newTestApp(t)
	if !press(a, "q") {
		t.Fatal("q quits a clean app")
	}
	b, _ := newTestApp(t)
	if !press(b, "ctrl+c") {
		t.Fatal("ctrl+c quits a clean app")
	}

	// Keep editing.
	c, path := previewApp(t)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	selectRow(t, c, "font-size")
	press(c, "right")
	if press(c, "q") {
		t.Fatal("q must not quit with unsaved changes")
	}
	if c.ov != ovChanges || !c.chg.quit {
		t.Fatal("q opens the save dialog")
	}
	out := checkView(t, c, "quit dialog")
	for _, want := range []string{"save before quitting?", "font-size 16 → 17", "- font-size = 16", "+ font-size = 17", "save & quit", "discard & quit", "keep editing"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog should show %q", want)
		}
	}
	if press(c, "q") || c.ov != ovChanges {
		t.Error("q in the dialog does nothing")
	}
	press(c, "esc")
	if c.ov != ovNone || !c.dirty() || c.quitting {
		t.Error("esc keeps editing")
	}

	// Save & quit.
	if press(c, "ctrl+c") || c.ov != ovChanges {
		t.Fatal("ctrl+c with unsaved changes asks too")
	}
	if !press(c, "enter") {
		t.Fatal("⏎ saves and quits")
	}
	if !strings.Contains(readFile(t, path), "font-size = 17\n") || journal(c) {
		t.Error("save & quit")
	}

	// Discard & quit.
	d, path := previewApp(t)
	selectRow(t, d, "font-size")
	press(d, "right", "q")
	if !press(d, "d") {
		t.Fatal("d discards and quits")
	}
	if readFile(t, path) != sampleConfig || journal(d) {
		t.Error("discard & quit must leave the saved file")
	}

	// A second ⌃c in the dialog quits without saving.
	e, path := previewApp(t)
	selectRow(t, e, "font-size")
	press(e, "right", "ctrl+c")
	if !press(e, "ctrl+c") {
		t.Fatal("second ctrl+c quits")
	}
	if readFile(t, path) != sampleConfig {
		t.Error("second ctrl+c saved")
	}
}

func TestQuitAfterUndoingEverything(t *testing.T) {
	// A preview of an edit that was undone (but not yet re-previewed) must
	// not survive a plain quit.
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	a.set("font-size", []string{"16"}) // back to saved, still pending
	if a.dirty() {
		t.Fatal("nothing is unsaved")
	}
	if !press(a, "q") {
		t.Fatal("q quits")
	}
	if readFile(t, path) != sampleConfig || journal(a) {
		t.Errorf("the preview survived the quit:\n%s", readFile(t, path))
	}
}

func TestRevertOneChangeThenSave(t *testing.T) {
	// Whatever is left in the changelog at the end is what gets saved.
	a, path := newTestApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	a.set("cursor-style", []string{"bar"})
	press(a, "q")
	if len(a.changes()) != 2 {
		t.Fatalf("changes %+v", a.changes())
	}
	a.chg.cur = 0 // font-size
	press(a, "x")
	if cs := a.changes(); len(cs) != 1 || cs[0].key != "cursor-style" {
		t.Fatalf("after revert: %+v", cs)
	}
	if !press(a, "enter") {
		t.Fatal("save & quit")
	}
	got := readFile(t, path)
	if !strings.Contains(got, "cursor-style = bar\n") || !strings.Contains(got, "font-size = 16\n") {
		t.Errorf("file:\n%s", got)
	}
}

func TestChangesOverlay(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	a.set("font-size", []string{"18"})
	a.set("window-padding-y", []string{"4"})
	a.set("window-padding-x", nil)
	drive(a, a.flush())
	press(a, "s")
	out := checkView(t, a, "changes")
	for _, want := range []string{"unsaved changes", "3 ·", "font-size 16 → 18", "line 3", "- font-size = 16", "+ font-size = 18",
		"window-padding-y → 4", "new", "+ window-padding-y = 4", "window-padding-x 5 → default", "- window-padding-x = 5", "Nothing is written until you save."} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay should show %q", want)
		}
	}
	press(a, "down", "down", "down")
	if a.chg.cur != 2 {
		t.Errorf("cursor %d", a.chg.cur)
	}
	press(a, "q")
	if a.ov != ovNone {
		t.Error("q closes the review overlay")
	}
	press(a, "s", "enter")
	if a.ov != ovNone || a.dirty() || a.status != "saved 3 changes" {
		t.Errorf("⏎ saves: %q", a.status)
	}
	press(a, "s")
	if out := checkView(t, a, "empty"); !strings.Contains(out, "No unsaved changes.") {
		t.Error("empty overlay")
	}
}

func TestShutdownKeepsChangesForNextStart(t *testing.T) {
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	if err := a.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, path) != sampleConfig {
		t.Error("shutdown must take the preview off the disk")
	}
	if !journal(a) {
		t.Fatal("shutdown keeps the unsaved changes")
	}
	if err := a.Shutdown(); err != nil {
		t.Error("a second shutdown is a no-op")
	}

	b := openApp(t, path, Options{})
	if v := b.vals("font-size"); v[0] != "17" || !b.dirty() {
		t.Fatalf("not recovered: %q", v)
	}
	drive(b, b.flush())
	if b.status != "recovered 1 unsaved change from the last session" {
		t.Errorf("status %q", b.status)
	}
	if !b.dirty() || readFile(t, path) != sampleConfig {
		t.Error("recovered changes stay unsaved")
	}
	press(b, "q")
	if b.ov != ovChanges {
		t.Error("recovered changes are asked about on quit")
	}
}

func TestCrashWithPreviewOnDisk(t *testing.T) {
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	// The process dies: no Shutdown.
	b := openApp(t, path, Options{Preview: true})
	if readFile(t, path) != sampleConfig {
		t.Error("the next start must put the saved config back first")
	}
	if v := b.vals("font-size"); v[0] != "17" {
		t.Errorf("not recovered: %q", v)
	}
	drive(b, b.flush())
	if !strings.Contains(readFile(t, path), "font-size = 17\n") {
		t.Error("with preview on, the recovered change is shown again")
	}
}

func TestOutsideEditDuringPreview(t *testing.T) {
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	writeOutside(t, path, readFile(t, path)+"window-padding-y = 3\n") // nvim, behind the preview
	drive(a, func() tea.Msg { return pollMsg{} })
	if v := a.file.Values("window-padding-y"); len(v) != 1 || v[0] != "3" {
		t.Fatalf("the outside edit becomes part of the saved config: %q", v)
	}
	if v := a.file.Values("font-size"); v[0] != "16" {
		t.Errorf("the unsaved change must not become saved: %q", v)
	}
	if u := a.unsaved(); !slices.Equal(u, []string{"font-size"}) {
		t.Errorf("unsaved %q", u)
	}
	press(a, "s", "d")
	if got := readFile(t, path); got != sampleConfig+"window-padding-y = 3\n" {
		t.Errorf("discard keeps the outside edit:\n%s", got)
	}
}

func TestSaveAfterOutsideEdit(t *testing.T) {
	// Without polling in between, the save notices the outside edit, takes
	// it in, and asks for another save.
	a, path := newTestApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	writeOutside(t, path, sampleConfig+"window-padding-y = 3\n")
	press(a, "ctrl+s")
	if a.statusKind != stErr || !strings.Contains(a.status, "changed on disk") {
		t.Fatalf("status %q", a.status)
	}
	if got := readFile(t, path); got != sampleConfig+"window-padding-y = 3\n" {
		t.Fatalf("the outside edit was overwritten:\n%s", got)
	}
	press(a, "ctrl+s")
	got := readFile(t, path)
	if !strings.Contains(got, "font-size = 17\n") || !strings.Contains(got, "window-padding-y = 3\n") {
		t.Errorf("second save:\n%s", got)
	}
}

func TestPollPicksUpExternalEdits(t *testing.T) {
	a, path := newTestApp(t)
	writeOutside(t, path, "font-size = 22\n")
	drive(a, func() tea.Msg { return pollMsg{} })
	if v := a.file.Values("font-size"); len(v) != 1 || v[0] != "22" {
		t.Fatalf("external edit not picked up: %q", v)
	}
	if !strings.Contains(a.status, "changed on disk") {
		t.Errorf("status %q", a.status)
	}
	a.pending["theme"] = []string{"X"}
	writeOutside(t, path, "font-size = 23\ntheme = Y\n")
	a.Update(pollMsg{})
	if v := a.file.Values("font-size"); v[0] != "22" {
		t.Error("poll must wait while an edit is pending")
	}
}

func TestStepNum(t *testing.T) {
	a, _ := newTestApp(t)
	op := a.schema.Get("background-opacity")
	for _, c := range []struct {
		v    float64
		dir  int
		want string
	}{{0.92, 1, "0.95"}, {0.92, -1, "0.9"}, {0.9, 1, "0.95"}, {1, 1, "1"}, {0, -1, "0"}} {
		if got := fmtNum(op, stepNum(op, c.v, c.dir, false)); got != c.want {
			t.Errorf("opacity %v %+d → %s, want %s", c.v, c.dir, got, c.want)
		}
	}
	pad := a.schema.Get("window-padding-x")
	if got := fmtNum(pad, stepNum(pad, 0, -1, false)); got != "0" {
		t.Errorf("padding below 0: %s", got)
	}
	if got := fmtNum(pad, 2.6); got != "3" {
		t.Errorf("int formatting %s", got)
	}
	if got := fmtNum(op, 0.1+0.2); got != "0.3" {
		t.Errorf("float formatting %s", got)
	}
}

func TestTriggerFor(t *testing.T) {
	for _, c := range []struct {
		k    tea.Key
		want string
	}{
		{tea.Key{Code: 'g', Mod: tea.ModCtrl | tea.ModAlt}, "ctrl+alt+g"},
		{tea.Key{Code: 'A', Mod: tea.ModShift}, "shift+a"},
		{tea.Key{Code: 'G'}, "shift+g"},
		{tea.Key{Code: tea.KeyUp, Mod: tea.ModSuper}, "super+arrow_up"},
		{tea.Key{Code: tea.KeyF1 + 4}, "f5"},
		{tea.Key{Code: '=', Mod: tea.ModSuper}, "super+="},
		{tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}, "shift+enter"},
		{tea.Key{Code: tea.KeyEscape}, ""},
	} {
		if got := triggerFor(c.k); got != c.want {
			t.Errorf("%+v → %q, want %q", c.k, got, c.want)
		}
	}
}

func TestDescribeChange(t *testing.T) {
	for _, c := range []struct {
		key           string
		before, after []string
		want          string
	}{
		{"font-size", []string{"13"}, []string{"14"}, "font-size 13 → 14"},
		{"font-size", nil, []string{"14"}, "font-size → 14"},
		{"font-size", []string{"13"}, nil, "font-size 13 → default"},
		{"title", nil, []string{" "}, `title → " "`},
		{"keybind", []string{"a=b"}, []string{"a=b", "ctrl+a=new_tab"}, "keybind + ctrl+a=new_tab"},
		{"keybind", []string{"a=b", "c=d"}, []string{"a=b"}, "keybind − c=d"},
		{"theme", []string{"A"}, []string{"light:L,dark:D"}, "theme A → L ◐ D"},
	} {
		if got := plain(describeChange(c.key, c.before, c.after)); got != c.want {
			t.Errorf("%s %q → %q: %q, want %q", c.key, c.before, c.after, got, c.want)
		}
	}
}

func TestSaveFromAnywhere(t *testing.T) {
	a, path := newTestApp(t)
	a.open(a.schema.Get("background"))
	a.ed.text.Set("")
	typeText(a, "#112233")
	press(a, "ctrl+s") // without ⏎ first
	if !strings.Contains(readFile(t, path), "background = #112233\n") {
		t.Errorf("⌃s in the editor should keep the typed value and save:\n%s", readFile(t, path))
	}
	a.openFonts(a.schema.Get("font-family"))
	a.set("font-size", []string{"12"})
	press(a, "ctrl+s")
	if !strings.Contains(readFile(t, path), "font-size = 12\n") || a.ov != ovFonts {
		t.Error("⌃s saves from an overlay and leaves it open")
	}
}

func TestRejectedEditCancelsTheSaveWaitingForIt(t *testing.T) {
	a, path := newTestApp(t)
	a.set("font-size", []string{"12"})
	drive(a, a.flush())
	before := readFile(t, path)
	a.open(a.schema.Get("background"))
	a.ed.text.Set("notacolor")
	press(a, "ctrl+s")
	if readFile(t, path) != before {
		t.Error("a save waiting on a rejected edit must not run")
	}
	if !strings.Contains(a.status, "not applied") || !strings.HasSuffix(a.status, "nothing saved") {
		t.Errorf("the error must stay visible: %q", a.status)
	}
	if !a.dirty() {
		t.Error("the valid unsaved change is still there")
	}

	// Same from the quit dialog: no quit, the dialog stays.
	press(a, "esc")
	a.set("background", []string{"notacolor"})
	press(a, "q")
	if a.ov != ovChanges {
		t.Fatal("dialog")
	}
	if press(a, "enter") {
		t.Error("save & quit must not quit when the save didn't happen")
	}
	if a.quitting || a.ov != ovChanges {
		t.Errorf("quitting %v, overlay %v", a.quitting, a.ov)
	}
}

func TestFontPickerWithNoMatch(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.openFonts(a.schema.Get("font-family"))
	typeText(a, "zzqqxx")
	if len(a.fp.res) != 0 {
		t.Skipf("a font matches %q", "zzqqxx")
	}
	press(a, "down", "backspace", "enter", "tab") // used to index res[-1]
	if a.fp.cur != 0 {
		t.Errorf("cursor %d", a.fp.cur)
	}
}

func TestListEntryRejectedWhileTyping(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.open(a.schema.Get("palette"))
	a.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	for _, r := range "99=nothex" {
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, check := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // pending, not validated yet
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})             // start editing that entry
	drive(a, check)                                           // rejected: the entry goes
	if len(a.vals("palette")) != 0 {
		t.Fatalf("palette %q", a.vals("palette"))
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // used to panic
	drive(a, cmd)
	if a.statusKind != stErr {
		t.Errorf("what was typed is checked again as a new entry: %q", a.status)
	}
}

func TestArrowsKeepValuesThatArentPlainNumbers(t *testing.T) {
	a, _ := newTestApp(t)
	for key, v := range map[string]string{"window-padding-x": "2,4", "adjust-cell-height": "10%"} {
		o := a.schema.Get(key)
		a.set(key, []string{v})
		drive(a, a.flush())
		drive(a, a.adjust(o, 1, false))
		if got := a.vals(key); len(got) != 1 || got[0] != v {
			t.Errorf("%s: %q became %q", key, v, got)
		}
	}
	o := a.schema.Get("adjust-cell-width") // unset, no default: steps from 0
	drive(a, a.adjust(o, 1, false))
	if got := a.vals(o.Key); len(got) != 1 || got[0] != "1" {
		t.Errorf("unset value: %q", got)
	}
}

func TestBackToSavedAfterOutsideEdit(t *testing.T) {
	// An edit lands behind the preview, then the change is taken back before
	// the poll notices: the edit must survive the next save.
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	writeOutside(t, path, readFile(t, path)+"window-padding-y = 3\n")
	press(a, "left")
	if v := a.file.Values("window-padding-y"); len(v) != 1 || v[0] != "3" {
		t.Fatalf("the outside edit becomes part of the saved config: %q", v)
	}
	if got := readFile(t, path); got != sampleConfig+"window-padding-y = 3\n" {
		t.Errorf("disk:\n%s", got)
	}
	selectRow(t, a, "cursor-style")
	press(a, "right", "ctrl+s")
	if !strings.Contains(readFile(t, path), "window-padding-y = 3\n") {
		t.Errorf("outside edit lost after save:\n%s", readFile(t, path))
	}
}

func TestHandEditOfAnUnsavedKeyWins(t *testing.T) {
	for _, preview := range []bool{true, false} {
		a, path := newTestApp(t)
		a.opt.Preview, a.opt.Reload = preview, preview
		selectRow(t, a, "font-size")
		press(a, "right") // 17, unsaved
		writeOutside(t, path, strings.Replace(readFile(t, path), "font-size = "+map[bool]string{true: "17", false: "16"}[preview], "font-size = 20", 1))
		drive(a, func() tea.Msg { return pollMsg{} })
		if v := a.vals("font-size"); len(v) != 1 || v[0] != "20" {
			t.Errorf("preview %v: working %q", preview, v)
		}
		if !strings.Contains(a.status, "font-size") {
			t.Errorf("preview %v: status %q", preview, a.status)
		}
		press(a, "q")
		if got := config.Parse([]byte(readFile(t, path))).Values("font-size"); len(got) != 1 || got[0] != "20" {
			t.Errorf("preview %v: font-size on disk %q", preview, got)
		}
	}
}

func TestIncludedKeysAreMarked(t *testing.T) {
	if _, err := ghostty.Find(); err != nil {
		t.Skip("ghostty not installed")
	}
	t.Setenv("GHOSTUI_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(dir+"/colors", []byte("cursor-style = underline\n"), 0o644)
	path := dir + "/config"
	os.WriteFile(path, []byte("cursor-style = block\nconfig-file = colors\n"), 0o644)
	a := openApp(t, path, Options{})
	if !strings.Contains(a.note, "included files") {
		t.Errorf("note %q", a.note)
	}
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	a.setTab(1) // appearance
	selectRow(t, a, "cursor-style")
	out := checkView(t, a, "appearance")
	if !strings.Contains(out, "include") || !strings.Contains(out, "Ghostty uses underline from colors") {
		t.Errorf("the included value isn't flagged:\n%s", out)
	}
	if v := a.effective("cursor-style"); v != "underline" {
		t.Errorf("effective %q", v)
	}
}

func TestAppleScriptOffTurnsPreviewOff(t *testing.T) {
	if _, err := ghostty.Find(); err != nil {
		t.Skip("ghostty not installed")
	}
	t.Setenv("GHOSTUI_STATE_DIR", t.TempDir())
	path := t.TempDir() + "/config"
	os.WriteFile(path, []byte("macos-applescript = false\n"), 0o644)
	a := openApp(t, path, Options{Preview: true, Reload: true})
	if a.opt.Preview || a.opt.Reload || !strings.Contains(a.note, "macos-applescript") {
		t.Errorf("preview %v reload %v note %q", a.opt.Preview, a.opt.Reload, a.note)
	}
}

func TestReloadWithoutGhosttyRunningIsQuiet(t *testing.T) {
	prev := reloadGhostty
	reloadGhostty = func(context.Context) error { return ghostty.ErrNotRunning }
	t.Cleanup(func() { reloadGhostty = prev })
	a, path := previewApp(t)
	selectRow(t, a, "font-size")
	press(a, "right")
	if a.statusKind == stErr {
		t.Errorf("preview: %q", a.status)
	}
	press(a, "ctrl+s")
	if a.statusKind == stErr || !strings.Contains(readFile(t, path), "font-size = 17\n") {
		t.Errorf("save: %q", a.status)
	}
	press(a, "R")
	if a.statusKind == stErr {
		t.Errorf("R: %q", a.status)
	}
}

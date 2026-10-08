package ui

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Cflex96/ghostui/internal/config"
	"github.com/Cflex96/ghostui/internal/ghostty"
	"github.com/Cflex96/ghostui/internal/state"
)

// Edits are unsaved until the human saves them. Each edit is validated (and,
// with Preview, written to the file so Ghostty shows it) after a short
// debounce; saving makes the working copy the saved config, and discarding
// puts the saved config back. config.Draft does every write.

type (
	stagedMsg struct {
		snap      config.Snapshot
		wrote     bool
		err       error
		reloaded  bool
		reloadErr error
	}
	savedMsg struct {
		snap      config.Snapshot
		n         int
		err       error
		reloaded  bool
		reloadErr error
	}
	discardedMsg struct {
		snap     config.Snapshot
		wrote    bool
		n        int
		err      error
		reloaded bool
	}
)

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func (a *App) touch(key string) {
	if !slices.Contains(a.order, key) {
		a.order = append(a.order, key)
	}
}

// tidy forgets keys that no longer differ from the saved config.
func (a *App) tidy() {
	a.order = slices.DeleteFunc(a.order, func(k string) bool {
		_, p := a.pending[k]
		_, i := a.inflight[k]
		_, s := a.staged[k]
		return !p && !i && !s
	})
}

// working is the saved config with every unsaved change applied: what the
// preview shows and what saving writes.
func (a *App) working() *config.File {
	f := a.file.Clone()
	for _, k := range a.order {
		f.Replace(k, nil, a.vals(k))
	}
	return f
}

// unsaved lists the keys whose values differ from the saved config, in the
// order they were first changed.
func (a *App) unsaved() []string {
	var out []string
	for _, k := range a.order {
		if !slices.Equal(a.vals(k), a.file.Values(k)) {
			out = append(out, k)
		}
	}
	return out
}

func (a *App) dirty() bool { return len(a.unsaved()) > 0 }

func (a *App) unsavedValues() map[string][]string {
	m := map[string][]string{}
	for _, k := range a.unsaved() {
		m[k] = a.vals(k)
	}
	return m
}

func (a *App) idle() bool { return !a.busy && len(a.pending) == 0 }

// deferred is an action waiting for the app to be idle. Saves, undos and
// redos are dropped when the edits before them are rejected; a discard
// (firm) runs anyway.
type deferred struct {
	run  func() tea.Cmd
	firm bool
}

// later runs f once nothing is pending or busy.
func (a *App) later(f func() tea.Cmd) { a.then = append(a.then, deferred{run: f}) }

// dropDeferred forgets the actions that depended on rejected edits and
// reports whether any did.
func (a *App) dropDeferred() bool {
	n := len(a.then)
	a.then = slices.DeleteFunc(a.then, func(d deferred) bool { return !d.firm })
	if len(a.then) == n {
		return false
	}
	if len(a.then) == 0 {
		a.quitting = false // the save & quit didn't happen
	}
	return true
}

// next starts what waits: pending edits first, then deferred actions.
func (a *App) next() tea.Cmd {
	var cmds []tea.Cmd
	for !a.busy {
		if len(a.pending) > 0 {
			cmds = append(cmds, a.flush())
			continue
		}
		if len(a.then) == 0 {
			break
		}
		d := a.then[0]
		a.then = a.then[1:]
		cmds = append(cmds, d.run())
	}
	return tea.Batch(cmds...)
}

// set changes key's values (nil unsets it). Validation and the preview are
// debounced so a burst of arrow presses is one check, one write, one reload.
func (a *App) set(key string, vals []string) tea.Cmd {
	a.pending[key] = nonNil(vals)
	a.touch(key)
	a.redo = nil
	a.saveSeq++
	seq := a.saveSeq
	return tick(280*time.Millisecond, func(time.Time) tea.Msg { return saveTickMsg{seq} })
}

// commit applies explicit changes (see config.File.Replace) to the working
// copy right away.
func (a *App) commit(changes ...config.Change) tea.Cmd {
	for _, c := range changes {
		a.restage(c.Key, c.From, c.To)
	}
	a.redo = nil
	return a.flush()
}

// restage applies one from → to change to key's working values.
func (a *App) restage(key string, from, to []string) {
	f := a.working()
	f.Replace(key, from, to)
	a.pending[key] = nonNil(f.Values(key))
	a.touch(key)
}

// flush validates the pending edits and, with Preview, shows them.
func (a *App) flush() tea.Cmd {
	if a.busy || len(a.pending) == 0 {
		return nil
	}
	a.inflight, a.pending = a.pending, map[string][]string{}
	a.busy = true
	data, staged := a.working().Bytes(), a.unsavedValues()
	d, preview, reload := a.draft, a.opt.Preview, a.opt.Reload
	return func() tea.Msg {
		var m stagedMsg
		if preview {
			m.snap, m.wrote, m.err = d.Show(bg, data, staged)
		} else {
			m.err = d.Check(bg, data, staged)
		}
		if m.err == nil && m.wrote && reload {
			m.reloaded, m.reloadErr = reloadIfRunning()
		}
		return m
	}
}

func (a *App) onStaged(m stagedMsg) tea.Cmd {
	a.busy = false
	inflight := a.inflight
	a.inflight = map[string][]string{}
	kind, entry := a.batch, a.batchEntry
	a.batch = batchNormal
	var cmds []tea.Cmd
	if m.wrote {
		a.modTime, a.size, a.checked = m.snap.ModTime, m.snap.Size, false
		cmds = append(cmds, a.check())
	}
	if m.reloaded && m.reloadErr == nil {
		cmds = append(cmds, recolor())
	}

	switch {
	case errors.Is(m.err, config.ErrChanged):
		// Edited behind our back: take that edit in, then try again.
		for k, v := range inflight {
			if _, ok := a.pending[k]; !ok {
				a.pending[k] = v
			}
		}
		a.batch, a.batchEntry = kind, entry
		cmds = append(cmds, a.rebase())
	case m.err != nil:
		switch kind {
		case batchUndo:
			a.steps = append(slices.Clip(a.steps), entry)
			a.redo = a.redo[:len(a.redo)-1]
		case batchRedo:
			a.steps = a.steps[:len(a.steps)-1]
			a.redo = append(a.redo, entry)
		}
		msg := "✗ " + m.err.Error()
		var inv *config.InvalidError
		if errors.As(m.err, &inv) {
			msg = "✗ not applied: " + inv.Error()
		}
		if a.dropDeferred() {
			msg += " · nothing saved"
		}
		cmds = append(cmds, a.flash(msg, stErr))
	default:
		now := time.Now()
		changed := 0
		for _, k := range a.order {
			after, ok := inflight[k]
			if !ok {
				continue
			}
			before := a.file.Values(k)
			if v, ok := a.staged[k]; ok {
				before = v
			}
			if slices.Equal(before, after) {
				continue
			}
			changed++
			if kind == batchNormal {
				a.steps = state.Add(a.steps, a.opt.Path, k, before, after, now)
			}
			if slices.Equal(after, a.file.Values(k)) {
				delete(a.staged, k)
			} else {
				a.staged[k] = after
			}
		}
		cmds = append(cmds, a.flash(a.stagedStatus(kind, entry, changed, m)))
	}
	a.tidy()
	cmds = append(cmds, a.next())
	return tea.Batch(cmds...)
}

func (a *App) stagedStatus(kind batchKind, entry state.Entry, changed int, m stagedMsg) (string, statusKind) {
	if m.reloaded && m.reloadErr != nil {
		return "preview reload failed: " + m.reloadErr.Error(), stErr
	}
	msg := "not saved yet · ⌃s saves"
	if a.opt.Preview {
		msg = "previewing · ⌃s saves"
	}
	switch {
	case a.note != "":
		msg, a.note = a.note, ""
	case kind == batchUndo:
		msg = "undid " + entry.Key
	case kind == batchRedo:
		msg = "redid " + entry.Key
	case changed == 0:
		msg = "no change"
	case !a.dirty():
		msg = "matches the saved config"
	}
	return msg, stInfo
}

// save writes every unsaved change to the config.
func (a *App) save() tea.Cmd {
	if !a.idle() {
		a.later(a.save)
		return a.flush()
	}
	n := len(a.unsaved())
	if n == 0 {
		if a.quitting {
			return tea.Quit
		}
		return a.flash("nothing to save", stInfo)
	}
	data := a.working().Bytes()
	a.busy = true
	d, reload := a.draft, a.opt.Reload
	return func() tea.Msg {
		snap, changed, err := d.Save(bg, data)
		m := savedMsg{snap: snap, n: n, err: err}
		if err == nil && changed && reload {
			m.reloaded, m.reloadErr = reloadIfRunning()
		}
		return m
	}
}

func (a *App) onSaved(m savedMsg) tea.Cmd {
	a.busy = false
	var cmds []tea.Cmd
	var inv *config.InvalidError
	switch {
	case errors.Is(m.err, config.ErrChanged):
		a.quitting = false
		cmds = append(cmds, a.rebase(), a.flash("✗ not saved: the config changed on disk · check the changes, then save again", stErr))
	case errors.As(m.err, &inv):
		a.quitting = false
		cmds = append(cmds, a.flash("✗ not saved: "+inv.Error(), stErr))
	case m.err != nil:
		a.quitting = false
		cmds = append(cmds, a.flash("✗ not saved: "+m.err.Error(), stErr))
	default:
		a.file, a.modTime, a.size, a.checked = m.snap.File, m.snap.ModTime, m.snap.Size, false
		a.staged = map[string][]string{}
		a.tidy()
		a.hist.Replace(a.opt.Path, a.steps)
		a.savedSteps = slices.Clone(a.steps)
		msg, st := "saved "+plural(m.n, "change"), stOK
		switch {
		case m.reloaded && m.reloadErr != nil:
			msg, st = msg+" · reload failed: "+m.reloadErr.Error(), stErr
		case m.reloaded:
			msg += " · reloaded"
			cmds = append(cmds, recolor())
		}
		cmds = append(cmds, a.flash(msg, st), a.check())
		if a.quitting && a.idle() && !a.dirty() {
			return tea.Batch(append(cmds, tea.Quit)...)
		}
	}
	cmds = append(cmds, a.next())
	return tea.Batch(cmds...)
}

// discard drops every unsaved change and puts the saved config back.
func (a *App) discard() tea.Cmd {
	if a.busy {
		a.then = append(a.then, deferred{run: a.discard, firm: true})
		return nil
	}
	n := len(a.unsaved())
	a.pending, a.staged, a.order = map[string][]string{}, map[string][]string{}, nil
	a.steps, a.redo = slices.Clone(a.savedSteps), nil
	a.busy = true
	d, reload := a.draft, a.opt.Reload
	return func() tea.Msg {
		snap, wrote, err := d.Discard()
		m := discardedMsg{snap: snap, wrote: wrote, n: n, err: err}
		if err == nil && wrote && reload {
			ok, err := reloadIfRunning()
			m.reloaded = ok && err == nil
		}
		return m
	}
}

func (a *App) onDiscarded(m discardedMsg) tea.Cmd {
	a.busy = false
	var cmds []tea.Cmd
	switch {
	case m.err != nil:
		a.quitting = false
		cmds = append(cmds, a.flash("✗ couldn't put the saved config back: "+m.err.Error(), stErr))
	default:
		a.file = m.snap.File
		if m.wrote {
			a.modTime, a.size, a.checked = m.snap.ModTime, m.snap.Size, false
			cmds = append(cmds, a.check())
		}
		if a.quitting {
			return tea.Batch(append(cmds, tea.Quit)...)
		}
		if m.reloaded {
			cmds = append(cmds, recolor())
		}
		if m.n > 0 {
			msg := "discarded " + plural(m.n, "change")
			if m.reloaded {
				msg += " · reloaded"
			}
			cmds = append(cmds, a.flash(msg, stInfo))
		}
	}
	cmds = append(cmds, a.next())
	return tea.Batch(cmds...)
}

// undo steps back one change. Changes saved in earlier sessions can be
// undone too; undoing stages the old value, it doesn't save it.
func (a *App) undo() tea.Cmd {
	if !a.idle() {
		a.later(a.undo)
		return a.flush()
	}
	if len(a.steps) == 0 {
		return a.flash("nothing to undo", stInfo)
	}
	e := a.steps[len(a.steps)-1]
	a.steps = a.steps[:len(a.steps)-1]
	a.redo = append(a.redo, e)
	a.batch, a.batchEntry = batchUndo, e
	a.restage(e.Key, e.After, e.Before)
	return a.flush()
}

func (a *App) redoLast() tea.Cmd {
	if !a.idle() {
		a.later(a.redoLast)
		return a.flush()
	}
	if len(a.redo) == 0 {
		return a.flash("nothing to redo", stInfo)
	}
	e := a.redo[len(a.redo)-1]
	a.redo = a.redo[:len(a.redo)-1]
	a.steps = append(slices.Clip(a.steps), e)
	a.batch, a.batchEntry = batchRedo, e
	a.restage(e.Key, e.Before, e.After)
	return a.flush()
}

// quit leaves right away when nothing is unsaved, and asks otherwise.
func (a *App) quit() tea.Cmd {
	if a.dirty() {
		a.openChanges(true)
		return nil
	}
	// Nothing worth keeping, but a preview of an edit that was just undone
	// may still be on disk: put the saved config back on the way out.
	a.quitting = true
	return a.discard()
}

// poll notices edits made outside the TUI (say, in an editor).
func (a *App) poll() tea.Cmd {
	if !a.idle() {
		return nil
	}
	mt, size := a.store.Stat()
	if mt.Equal(a.modTime) && size == a.size {
		return nil
	}
	return tea.Batch(a.rebase(), a.next())
}

// rebase takes in an edit made outside the TUI. It becomes part of the saved
// config; unsaved changes stay unsaved and, with Preview, are shown again on
// top of it. Where the edit changed a value that has an unsaved change, the
// edit wins and the unsaved change goes.
func (a *App) rebase() tea.Cmd {
	old := a.file
	snap, err := a.draft.Rebase()
	if err != nil {
		return nil
	}
	a.file, a.modTime, a.size, a.checked = snap.File, snap.ModTime, snap.Size, false
	var dropped []string
	onto := func(k string, v []string) []string {
		nv := config.Patch(a.file.Values(k), old.Values(k), v)
		if !slices.Equal(v, old.Values(k)) && !slices.Equal(nv, v) && slices.Equal(nv, a.file.Values(k)) && !slices.Contains(dropped, k) {
			dropped = append(dropped, k)
		}
		return nonNil(nv)
	}
	for k, v := range a.pending {
		a.pending[k] = onto(k, v)
	}
	for k, v := range a.staged {
		nv := onto(k, v)
		if slices.Equal(nv, a.file.Values(k)) {
			delete(a.staged, k)
			continue
		}
		a.staged[k] = nv
		if _, ok := a.pending[k]; !ok && (a.opt.Preview || !slices.Equal(nv, v)) {
			a.pending[k] = nv
		}
	}
	a.tidy()
	msg := "config changed on disk · view updated"
	if len(dropped) > 0 {
		sort.Strings(dropped)
		msg = "config changed on disk · kept that edit of " + strings.Join(dropped, ", ") + " over the unsaved one"
	}
	return tea.Batch(a.check(), a.flash(msg, stInfo))
}

// Shutdown takes a preview off the disk and keeps the unsaved changes for
// the next start. main calls it however the program ends (quit, SIGTERM,
// SIGHUP); it is safe from any goroutine and more than once.
func (a *App) Shutdown() error { return a.draft.Close() }

// reloadIfRunning asks Ghostty to reload its config. A Ghostty that isn't
// running, or has no window, has nothing to show yet: that isn't an error,
// and reloaded is false.
func reloadIfRunning() (reloaded bool, err error) {
	err = reloadGhostty(bg)
	if errors.Is(err, ghostty.ErrNotRunning) {
		return false, nil
	}
	return true, err
}

// reloadNow asks Ghostty to reload whatever is on disk (R).
func (a *App) reloadNow() tea.Cmd {
	if !ghostty.CanReload() {
		return a.flash("live reload needs macOS", stInfo)
	}
	a.status, a.statusKind = "reloading…", stInfo
	return func() tea.Msg { return reloadedMsg{reloadGhostty(bg)} }
}

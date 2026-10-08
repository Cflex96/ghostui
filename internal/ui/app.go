// Package ui is the Bubble Tea settings TUI.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Cflex96/ghostui/internal/config"
	"github.com/Cflex96/ghostui/internal/ghostty"
	"github.com/Cflex96/ghostui/internal/state"
)

// Options configures the app.
type Options struct {
	Path    string       // config file to edit
	CLI     *ghostty.CLI // ghostty binary
	Preview bool         // write unsaved changes to the file so Ghostty shows them
	Reload  bool         // run reload_config after each write
}

type screen int

const (
	scrHome screen = iota
	scrThemes
	scrKeys
)

type overlayKind int

const (
	ovNone overlayKind = iota
	ovPalette
	ovEditor
	ovFonts
	ovHelp
	ovChanges
)

type tab struct {
	name string
	hot  string
}

var tabs = []tab{
	{"overview", "o"}, {"appearance", "a"}, {"font", "f"}, {"window", "w"},
	{"keys", "k"}, {"themes", "t"}, {"clipboard", "c"}, {"more", "m"},
}

const (
	tabOverview = 0
	tabKeys     = 4
	tabThemes   = 5
)

type statusKind int

const (
	stInfo statusKind = iota
	stOK
	stErr
)

type batchKind int

const (
	batchNormal batchKind = iota
	batchUndo
	batchRedo
)

// App is the root model.
type App struct {
	opt   Options
	store *config.Store
	draft *config.Draft
	hist  *state.History

	schema   *ghostty.Schema
	unknown  map[string]*ghostty.Option
	loadErr  error
	file     *config.File // the saved config
	modTime  time.Time    // of the file on disk, which holds the preview while there is one
	size     int64
	problems []string
	checked  bool

	inc   map[string]config.Include // keys set by config-file includes
	incOf *config.File              // the file inc was read for

	themes  []*ghostty.Theme
	themeBy map[string]*ghostty.Theme
	fonts   []string
	actions []ghostty.Action

	// Unsaved changes, one full value list per key. staged passed validation
	// (and, with Preview, is on disk); inflight is being validated; pending
	// waits for the debounce. order lists the keys they touch, in the order
	// they were first changed.
	staged     map[string][]string
	inflight   map[string][]string
	pending    map[string][]string
	order      []string
	busy       bool
	saveSeq    int
	batch      batchKind
	batchEntry state.Entry
	then       []deferred // run once nothing is pending or busy
	quitting   bool
	note       string // said once the app starts (recovered changes)
	oldGhostty bool   // the note already says the Ghostty is too old

	termBG, termFG color.Color // the terminal's colors, once it says

	// Undo: steps lead to the working copy, savedSteps to the saved file.
	steps      []state.Entry
	savedSteps []state.Entry
	redo       []state.Entry

	w, h    int
	scr     screen
	tab     int
	cursor  map[int]int
	scroll  map[int]int
	flagCur int

	ov   overlayKind
	pal  palette
	ed   editor
	th   themeStudio
	ks   keysScreen
	fp   fontPicker
	chg  changesView
	help int

	status     string
	statusKind statusKind
	statusSeq  int
}

// New creates the app. Unsaved changes a previous run left behind come back
// as unsaved changes; if that run left a preview on disk, the saved version
// is put back first.
func New(opt Options) *App {
	store := &config.Store{Path: opt.Path, Validate: opt.CLI.Validate}
	a := &App{
		opt:      opt,
		store:    store,
		draft:    config.NewDraft(store, state.Journal(opt.Path)),
		hist:     state.Open(),
		unknown:  map[string]*ghostty.Option{},
		staged:   map[string][]string{},
		inflight: map[string][]string{},
		pending:  map[string][]string{},
		cursor:   map[int]int{},
		scroll:   map[int]int{},
		themeBy:  map[string]*ghostty.Theme{},
	}
	rec, recErr := a.draft.Recover()
	snap, err := a.draft.Load()
	if err != nil {
		a.loadErr = err
	}
	a.file, a.modTime, a.size = snap.File, snap.ModTime, snap.Size
	a.steps = a.hist.For(opt.Path)
	a.savedSteps = slices.Clone(a.steps)
	keys := make([]string, 0, len(rec.Staged))
	for k := range rec.Staged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := rec.Staged[k]; !slices.Equal(v, a.file.Values(k)) {
			a.pending[k] = nonNil(v)
			a.touch(k)
		}
	}
	// Without a reload, a preview would only rewrite the file.
	if opt.Reload {
		switch v := opt.CLI.Version(bg); {
		case ghostty.OlderThan(v, minMajor, minMinor):
			a.opt.Preview, a.opt.Reload, a.oldGhostty = false, false, true
			a.note = fmt.Sprintf("live preview is off: it needs Ghostty %d.%d or newer, this is %s", minMajor, minMinor, v)
		case a.effective("macos-applescript") == "false":
			a.opt.Preview, a.opt.Reload = false, false
			a.note = "live preview is off: the config sets macos-applescript = false"
		}
	}
	switch {
	case recErr != nil:
		a.note = "couldn't read the unsaved changes of the last session: " + recErr.Error()
	case len(a.pending) > 0:
		a.note = "recovered " + plural(len(a.pending), "unsaved change") + " from the last session"
	case rec.Dropped:
		a.note = "dropped the last session's unsaved changes: the config changed since"
	case a.note == "" && len(a.file.Values("config-file")) > 0:
		a.note = "options set in included files are marked include"
	}
	a.ks.init()
	return a
}

// Messages from background work.
type (
	schemaMsg struct {
		s   *ghostty.Schema
		err error
	}
	themesMsg struct {
		t   []*ghostty.Theme
		err error
	}
	fontsMsg   struct{ f []string }
	actionsMsg struct{ a []ghostty.Action }
	checkMsg   struct {
		problems []string
		modTime  time.Time
	}
	saveTickMsg  struct{ seq int }
	colorsDueMsg struct{}
	statusOffMsg struct{ seq int }
	pollMsg      struct{}
	reloadedMsg  struct{ err error }
)

var bg = context.Background()

// Seams for tests: timers, the AppleScript reload, and asking the terminal
// for its colors.
var (
	tick          = tea.Tick
	reloadGhostty = ghostty.Reload
	askColors     = func() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, tea.RequestForegroundColor) }
)

// recolor asks the terminal for its colors again once Ghostty has had a
// moment to apply a reload, so the TUI follows a previewed theme.
func recolor() tea.Cmd {
	return tick(300*time.Millisecond, func(time.Time) tea.Msg { return colorsDueMsg{} })
}

func (a *App) Init() tea.Cmd {
	c := a.opt.CLI
	return tea.Batch(
		func() tea.Msg { s, err := ghostty.LoadSchema(bg, c); return schemaMsg{s, err} },
		func() tea.Msg { t, err := ghostty.ListThemes(bg, c); return themesMsg{t, err} },
		func() tea.Msg { f, _ := ghostty.ListFonts(bg, c); return fontsMsg{f} },
		askColors(),
		a.check(),
		poll(),
		a.announce(),
		a.flush(), // recovered changes: validate and preview them again
	)
}

// announce says what startup recovered. With recovered changes, the first
// preview's status says it instead (see stagedStatus), so it isn't
// overwritten at once.
func (a *App) announce() tea.Cmd {
	if a.note == "" || len(a.pending) > 0 || a.busy {
		return nil
	}
	note := a.note
	a.note = ""
	return a.flash(note, stInfo)
}

func poll() tea.Cmd {
	return tick(time.Second, func(time.Time) tea.Msg { return pollMsg{} })
}

// check validates the file as it is on disk.
func (a *App) check() tea.Cmd {
	path, c, mt, size := a.opt.Path, a.opt.CLI, a.modTime, a.size
	return func() tea.Msg {
		if size == 0 {
			return checkMsg{modTime: mt}
		}
		p, _ := c.Validate(bg, path)
		var out []string
		for _, l := range p {
			out = append(out, config.StripLocation(l))
		}
		return checkMsg{problems: out, modTime: mt}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		a.termBG = msg.Color
		setColors(a.termBG, a.termFG)
	case tea.ForegroundColorMsg:
		a.termFG = msg.Color
		setColors(a.termBG, a.termFG)
	case colorsDueMsg:
		return a, askColors()
	case schemaMsg:
		if msg.err != nil {
			a.loadErr = msg.err
			return a, nil
		}
		a.schema = msg.s
		s, c := msg.s, a.opt.CLI
		list := func() tea.Msg { acts, _ := ghostty.ListActions(bg, c, s); return actionsMsg{acts} }
		if ghostty.OlderThan(s.Version, minMajor, minMinor) && !a.oldGhostty {
			return a, tea.Batch(list, a.flash(fmt.Sprintf("ghostui is tested with Ghostty %d.%d · this is %s, so some things may not work", minMajor, minMinor, s.Version), stInfo))
		}
		return a, list
	case themesMsg:
		a.themes = msg.t
		for _, t := range msg.t {
			a.themeBy[t.Name] = t
		}
		a.th.refresh(a)
	case fontsMsg:
		a.fonts = msg.f
		if a.ov == ovFonts {
			a.fp.refresh(a)
		}
	case actionsMsg:
		a.actions = msg.a
		a.ks.refresh(a)
	case checkMsg:
		if msg.modTime.Equal(a.modTime) {
			a.problems, a.checked = msg.problems, true
		}
	case saveTickMsg:
		if msg.seq == a.saveSeq {
			return a, a.flush()
		}
	case stagedMsg:
		return a, a.onStaged(msg)
	case savedMsg:
		return a, a.onSaved(msg)
	case discardedMsg:
		return a, a.onDiscarded(msg)
	case reloadedMsg:
		if errors.Is(msg.err, ghostty.ErrNotRunning) {
			return a, a.flash("Ghostty isn't running · nothing to reload", stInfo)
		}
		if msg.err != nil {
			return a, a.flash("reload failed: "+msg.err.Error(), stErr)
		}
		return a, tea.Batch(a.flash("reloaded", stOK), recolor())
	case pollMsg:
		return a, tea.Batch(a.poll(), poll())
	case statusOffMsg:
		if msg.seq == a.statusSeq {
			a.status = ""
		}
	case tea.PasteMsg:
		if in := a.focusedInput(); in != nil {
			in.Insert(msg.Content)
			a.afterInput()
		}
	case tea.KeyPressMsg:
		return a, a.key(msg)
	}
	return a, nil
}

// --- values -----------------------------------------------------------------

// option returns the schema option for key, or a stand-in for keys Ghostty
// doesn't know (a typo in the file).
func (a *App) option(key string) *ghostty.Option {
	if o := a.schema.Get(key); o != nil {
		return o
	}
	if o := a.unknown[key]; o != nil {
		return o
	}
	o := &ghostty.Option{Key: key, Kind: ghostty.KString, Docs: "Ghostty doesn't know this option. It may be misspelled or from a newer version.", Defaults: []string{""}}
	o.Unit = "unknown"
	a.unknown[key] = o
	return o
}

// vals returns key's values including the unsaved changes.
func (a *App) vals(key string) []string {
	for _, m := range []map[string][]string{a.pending, a.inflight, a.staged} {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return a.file.Values(key)
}

func (a *App) overridden(key string) bool { return len(a.vals(key)) > 0 }

// cur is the effective value: the last entry in the file, else the default.
func (a *App) cur(o *ghostty.Option) string {
	if v := a.vals(o.Key); len(v) > 0 {
		return v[len(v)-1]
	}
	return o.Default()
}

// included reports what the config's includes set for key. They're read
// again whenever the saved config changes.
func (a *App) included(key string) (config.Include, bool) {
	if a.incOf != a.file {
		a.inc, a.incOf = a.store.Includes(a.file), a.file
	}
	inc, ok := a.inc[key]
	return inc, ok
}

// effective is the value Ghostty uses for key once the includes are loaded,
// or "" when nothing sets it.
func (a *App) effective(key string) string {
	v := a.vals(key)
	if inc, ok := a.included(key); ok {
		v = inc.Values
	}
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// list is a repeatable option's effective list: entries after the last ""
// reset.
func (a *App) list(key string) []string {
	v := a.vals(key)
	if i := slices.Index(slicesReverse(v), ""); i >= 0 {
		v = v[len(v)-i:]
	}
	return v
}

func slicesReverse(s []string) []string {
	r := slices.Clone(s)
	slices.Reverse(r)
	return r
}

// flash shows a status message in the footer for a few seconds.
func (a *App) flash(s string, k statusKind) tea.Cmd {
	a.status, a.statusKind = s, k
	a.statusSeq++
	seq := a.statusSeq
	d := 4 * time.Second
	if k == stErr {
		d = 9 * time.Second
	}
	return tick(d, func(time.Time) tea.Msg { return statusOffMsg{seq} })
}

// --- keys -------------------------------------------------------------------

func (a *App) key(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		if a.ov == ovChanges && a.chg.quit {
			// A second ⌃c: quit without saving.
			a.quitting = true
			return a.discard()
		}
		return a.quit()
	}
	if a.schema == nil {
		// Still loading: nothing to ask about yet. Unsaved changes recovered
		// from the last session stay in the journal for the next start.
		if s := msg.String(); s == "q" || s == "esc" {
			return tea.Quit
		}
		return nil
	}
	if msg.String() == "ctrl+s" && a.ov != ovChanges {
		// Saves from anywhere; a value being typed is part of it.
		var cmd tea.Cmd
		if a.ed.texting && (a.ov == ovEditor || a.ov == ovPalette) {
			cmd = a.commitText()
		}
		return tea.Batch(cmd, a.save())
	}
	switch a.ov {
	case ovChanges:
		return a.changesKey(msg)
	case ovPalette:
		return a.paletteKey(msg)
	case ovEditor:
		return a.editorKey(msg)
	case ovFonts:
		return a.fontKey(msg)
	case ovHelp:
		return a.helpKey(msg)
	}
	var cmd tea.Cmd
	handled := false
	switch a.scr {
	case scrHome:
		cmd, handled = a.homeKey(msg)
	case scrThemes:
		cmd, handled = a.themeKey(msg)
	case scrKeys:
		cmd, handled = a.keysKey(msg)
	}
	if handled {
		return cmd
	}
	return a.globalKey(msg)
}

func (a *App) globalKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	switch s {
	case "tab":
		return a.setTab((a.tab + 1) % len(tabs))
	case "shift+tab":
		return a.setTab((a.tab + len(tabs) - 1) % len(tabs))
	case "/":
		a.openPalette("")
		return nil
	case "?":
		a.ov, a.help = ovHelp, 0
		return nil
	case "u":
		return a.undo()
	case "U":
		return a.redoLast()
	case "s":
		a.openChanges(false)
		return nil
	case "R":
		return a.reloadNow()
	case "q":
		return a.quit()
	case "esc":
		if a.scr != scrHome {
			return a.setTab(tabOverview)
		}
		return nil
	}
	for i, t := range tabs {
		if s == t.hot {
			return a.setTab(i)
		}
	}
	return nil
}

func (a *App) setTab(i int) tea.Cmd {
	a.tab = i
	switch i {
	case tabKeys:
		a.scr = scrKeys
		a.ks.enter(a)
	case tabThemes:
		a.scr = scrThemes
		a.th.enter(a)
	default:
		a.scr = scrHome
	}
	a.flagCur = 0
	return nil
}

// focusedInput returns the text field that has focus, if any.
func (a *App) focusedInput() *input {
	switch {
	case a.ov == ovPalette && a.pal.focus == focusQuery:
		return &a.pal.q
	case (a.ov == ovPalette || a.ov == ovEditor) && a.ed.texting:
		return &a.ed.text
	case a.ov == ovFonts:
		return &a.fp.q
	case a.ov == ovNone && a.scr == scrThemes && a.th.filtering:
		return &a.th.q
	case a.ov == ovNone && a.scr == scrKeys && a.ks.stage == ksAction:
		return &a.ks.q
	}
	return nil
}

// afterInput refreshes whatever list the focused input filters.
func (a *App) afterInput() {
	switch {
	case a.ov == ovPalette && a.pal.focus == focusQuery:
		a.pal.refresh(a)
	case a.ov == ovFonts:
		a.fp.refresh(a)
	case a.scr == scrThemes:
		a.th.refresh(a)
	case a.scr == scrKeys:
		a.ks.refresh(a)
	}
}

// --- view -------------------------------------------------------------------

func (a *App) View() tea.View {
	var body string
	switch {
	case a.w == 0 || a.h == 0:
	case a.w < minWidth || a.h < minHeight:
		body = fit(sMuted.Render("terminal too small")+"\n"+sFaint.Render(fmt.Sprintf("%d×%d, needs %d×%d", a.w, a.h, minWidth, minHeight)), a.w, a.h)
	case a.loadErr != nil:
		body = a.viewError()
	case a.schema == nil:
		body = paint(fit("\n  "+sFaint.Render("reading ghostty +show-config …"), a.w, a.h), a.w, cBG)
	default:
		switch a.scr {
		case scrThemes:
			body = a.viewThemes()
		case scrKeys:
			body = a.viewKeys()
		default:
			body = a.viewHome()
		}
		body = paint(fit(body, a.w, a.h), a.w, cBG)
		switch a.ov {
		case ovPalette:
			body = a.withOverlay(body, a.viewPalette())
		case ovEditor:
			body = a.withOverlay(body, a.viewEditor())
		case ovFonts:
			body = a.withOverlay(body, a.viewFonts())
		case ovHelp:
			body = a.withOverlay(body, a.viewHelp())
		case ovChanges:
			body = a.withOverlay(body, a.viewChanges())
		}
	}
	v := tea.NewView(body)
	v.AltScreen = true
	v.WindowTitle = "ghostui — " + tilde(filepath.Dir(a.opt.Path))
	if a.dirty() {
		v.WindowTitle = "● " + v.WindowTitle
	}
	return v
}

// The Ghostty ghostui is tested with. Live preview needs its AppleScript
// support, which 1.3 added.
const minMajor, minMinor = 1, 3

// The smallest terminal the screens are laid out for.
const minWidth, minHeight = 30, 10

func (a *App) withOverlay(base, ov string) string {
	ow := 0
	for _, l := range lines(ov) {
		ow = max(ow, width(l))
	}
	x := max(0, (a.w-ow)/2)
	y := 2
	if a.h < 30 {
		y = 1
	}
	if oh := len(lines(ov)); y+oh > a.h {
		y = max(0, a.h-oh)
	}
	return overlay(base, ov, x, y, a.w)
}

func (a *App) viewError() string {
	msg := "could not start"
	if a.loadErr != nil {
		msg = a.loadErr.Error()
	}
	var b strings.Builder
	b.WriteString("\n  " + sBold.Render("ghostty") + sFaint.Render(" / ") + sMuted.Render("settings") + "\n\n")
	for _, l := range wrap(msg, a.w-6) {
		b.WriteString("  " + sErr.Render(l) + "\n")
	}
	b.WriteString("\n  " + keycap("q") + " " + sMuted.Render("quit"))
	return paint(fit(b.String(), a.w, a.h), a.w, cBG)
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

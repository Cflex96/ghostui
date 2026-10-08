package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// ErrChanged means the file on disk isn't what the draft last read or wrote:
// something else edited it.
var ErrChanged = errors.New("the config changed on disk")

// ErrClosed is returned by writes after Close.
var ErrClosed = errors.New("shutting down")

// Draft does every write to one config file on the TUI's behalf. Unsaved
// changes stay unsaved: they reach the disk only as a preview (Show), which
// Discard and Close take back out, or through Save.
//
// A journal next to the TUI's other state records the saved version and the
// unsaved changes before each preview is written, so if the process dies with
// a preview on disk, the next start (Recover) puts the saved version back.
//
// All methods take one lock, so a hangup arriving in the middle of a preview
// write can't race the restore.
type Draft struct {
	store   *Store
	journal string

	mu       sync.Mutex
	closed   bool
	saved    []byte              // the saved version
	existed  bool                // whether the saved version is a file (a preview may create it)
	shown    []byte              // the preview on disk; nil while the disk holds the saved version
	staged   map[string][]string // the unsaved values of the last Check or Show
	hashes   []string            // previews that may be on disk, newest last
	problems []string            // validation problems of the saved version
	probed   bool
}

// NewDraft returns a draft for the store's file. journal is the recovery
// file; "" disables it.
func NewDraft(s *Store, journal string) *Draft {
	return &Draft{store: s, journal: journal}
}

// Load reads the file and makes it the saved version.
func (d *Draft) Load() (Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, exists, err := d.store.read()
	if err != nil {
		return Snapshot{File: Parse(nil)}, err
	}
	d.setSaved(b, exists)
	d.shown = nil
	return d.snapshot(d.saved), nil
}

func (d *Draft) setSaved(b []byte, exists bool) {
	d.saved, d.existed = b, exists
	d.problems, d.probed = nil, false
}

// snapshot describes the disk, with f's content as the File.
func (d *Draft) snapshot(f []byte) Snapshot {
	_, exists, _ := d.store.read()
	snap := Snapshot{File: Parse(f), Exists: exists}
	snap.ModTime, snap.Size = d.store.Stat()
	return snap
}

// Previewing reports whether a preview is on disk.
func (d *Draft) Previewing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shown != nil
}

// Check validates data, the saved version plus the unsaved changes in
// staged, without writing it. Only problems the saved version doesn't have
// block it.
func (d *Draft) Check(ctx context.Context, data []byte, staged map[string][]string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	if !bytes.Equal(data, d.saved) {
		if err := d.validate(ctx, data); err != nil {
			return err
		}
	}
	d.staged = staged
	d.writeJournal() // best effort: nothing on disk needs repairing
	return nil
}

// Show validates data and writes it to the file as a preview. With data
// equal to the saved version, it puts the saved version back. wrote reports
// whether the file changed.
func (d *Draft) Show(ctx context.Context, data []byte, staged map[string][]string) (snap Snapshot, wrote bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return Snapshot{}, false, ErrClosed
	}
	// An edit made behind the preview has to reach the caller (Rebase)
	// before anything is written, even when the saved version goes back.
	disk, _, err := d.store.read()
	if err != nil {
		return Snapshot{}, false, err
	}
	if !bytes.Equal(disk, d.onDisk()) {
		return Snapshot{}, false, ErrChanged
	}
	if bytes.Equal(data, d.saved) {
		wrote, err = d.restore()
		if err == nil {
			d.staged, d.hashes = nil, nil
			d.dropJournal()
		}
		return d.snapshot(d.saved), wrote, err
	}
	if err := d.validate(ctx, data); err != nil {
		return Snapshot{}, false, err
	}
	d.staged = staged
	if bytes.Equal(disk, data) {
		return d.snapshot(data), false, d.writeJournal()
	}
	prev := d.hashes
	d.hashes = append(lastN(d.hashes, 1), hash(data))
	if err := d.writeJournal(); err != nil {
		d.hashes = prev
		return Snapshot{}, false, fmt.Errorf("can't write the recovery journal, so no preview: %w", err)
	}
	if err := d.store.write(data); err != nil {
		return Snapshot{}, false, err
	}
	d.shown = data
	return d.snapshot(data), true, nil
}

// Save validates data and writes it as the new saved version. changed
// reports whether the file's content changed (it doesn't when the preview
// already showed data).
func (d *Draft) Save(ctx context.Context, data []byte) (snap Snapshot, changed bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return Snapshot{}, false, ErrClosed
	}
	if err := d.validate(ctx, data); err != nil {
		return Snapshot{}, false, err
	}
	disk, exists, err := d.store.read()
	if err != nil {
		return Snapshot{}, false, err
	}
	if !bytes.Equal(disk, d.onDisk()) {
		return Snapshot{}, false, ErrChanged
	}
	changed = !bytes.Equal(disk, data)
	if changed || !exists {
		if err := d.store.write(data); err != nil {
			return Snapshot{}, false, err
		}
	}
	d.setSaved(data, true)
	d.shown, d.staged, d.hashes = nil, nil, nil
	d.dropJournal()
	return d.snapshot(data), changed, nil
}

// Discard drops the unsaved changes: it puts the saved version back if a
// preview is on disk, and removes the journal. The snapshot's File is the
// saved version; it differs from the one loaded when the file was edited
// behind the preview, because those edits are kept.
func (d *Draft) Discard() (snap Snapshot, wrote bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	wrote, err = d.restore()
	if err != nil {
		return Snapshot{}, false, err
	}
	d.staged, d.hashes = nil, nil
	d.dropJournal()
	return d.snapshot(d.saved), wrote, nil
}

// Close is Discard for shutting down: it puts the saved version back but
// keeps the journal, so the next start offers the unsaved changes again.
// Writes after Close fail with ErrClosed.
func (d *Draft) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	_, err := d.restore()
	if err == nil && len(d.staged) > 0 {
		err = d.writeJournal()
	}
	return err
}

// Rebase takes in an edit made outside the TUI. Without a preview on disk,
// the file is the new saved version. With one, the new saved version is the
// file with the unsaved changes taken back out, so they stay unsaved. The
// snapshot's File is the new saved version; ModTime and Size are the disk's.
func (d *Draft) Rebase() (Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	disk, exists, err := d.store.read()
	if err != nil {
		return Snapshot{}, err
	}
	switch {
	case d.shown == nil:
		d.setSaved(disk, exists)
	case !bytes.Equal(disk, d.shown):
		d.setSaved(d.unstage(disk), exists)
		d.shown = disk
		d.hashes = append(lastN(d.hashes, 1), hash(disk))
		d.writeJournal()
	}
	return d.snapshot(d.saved), nil
}

// onDisk is what the file should hold.
func (d *Draft) onDisk() []byte {
	if d.shown != nil {
		return d.shown
	}
	return d.saved
}

// restore puts the saved version back if a preview is on disk.
func (d *Draft) restore() (bool, error) {
	if d.shown == nil {
		return false, nil
	}
	disk, _, err := d.store.read()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(disk, d.shown) {
		// Edited behind the preview: take only the unsaved changes back out.
		d.setSaved(d.unstage(disk), true)
	}
	if err := d.put(d.saved, d.existed); err != nil {
		return false, err
	}
	d.shown = nil
	return true, nil
}

// put writes the saved version back; one that didn't exist is removed again.
func (d *Draft) put(saved []byte, existed bool) error {
	if !existed && len(saved) == 0 {
		err := os.Remove(d.store.real())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return d.store.write(saved)
}

// unstage returns disk with the unsaved changes reverted to their saved
// values, patching around anything else that changed. Where the edit made
// on disk changed an unsaved value itself, that edit wins.
func (d *Draft) unstage(disk []byte) []byte {
	f, saved := Parse(disk), Parse(d.saved)
	keys := make([]string, 0, len(d.staged))
	for k := range d.staged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f.Replace(k, d.staged[k], saved.Values(k))
	}
	return f.Bytes()
}

func (d *Draft) validate(ctx context.Context, data []byte) error {
	if d.store.Validate == nil {
		return nil
	}
	next, err := d.store.problems(ctx, data)
	if err != nil {
		return err
	}
	if !d.probed {
		var prev []string
		if len(d.saved) > 0 {
			if prev, err = d.store.problems(ctx, d.saved); err != nil {
				return err
			}
		}
		d.problems, d.probed = prev, true
	}
	if introduced := Introduced(d.problems, next); len(introduced) > 0 {
		return &InvalidError{Problems: introduced}
	}
	return nil
}

// --- journal ----------------------------------------------------------------

type journal struct {
	Path    string              `json:"path"`
	Saved   []byte              `json:"saved"`
	Existed bool                `json:"existed"`
	Shown   []string            `json:"shown"` // sha256 of previews that may be on disk
	Staged  map[string][]string `json:"staged"`
	Time    time.Time           `json:"time"`
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func lastN(s []string, n int) []string {
	return slices.Clone(s[max(0, len(s)-n):])
}

// writeJournal records the saved version and the unsaved changes, or
// removes the journal when there are none.
func (d *Draft) writeJournal() error {
	if d.journal == "" {
		return nil
	}
	if len(d.staged) == 0 && d.shown == nil {
		d.dropJournal()
		return nil
	}
	b, err := json.Marshal(journal{Path: d.store.Path, Saved: d.saved, Existed: d.existed, Shown: d.hashes, Staged: d.staged, Time: time.Now()})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.journal), 0o755); err != nil {
		return err
	}
	tmp := d.journal + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.journal)
}

func (d *Draft) dropJournal() {
	if d.journal != "" {
		os.Remove(d.journal)
	}
}

// Recovered is what Recover found from a previous run.
type Recovered struct {
	Staged   map[string][]string // unsaved changes to offer again
	Repaired bool                // the file still held a preview and was put back
	Dropped  bool                // the file changed since, so the changes were dropped
}

// Recover reads the journal a previous run left behind. If the file still
// holds one of its previews, the saved version goes back first. The unsaved
// changes are returned to be offered again, unless the file was edited since
// that run; then they're dropped. Call it before Load.
func (d *Draft) Recover() (Recovered, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var r Recovered
	if d.journal == "" {
		return r, nil
	}
	b, err := os.ReadFile(d.journal)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	var j journal
	if json.Unmarshal(b, &j) != nil || j.Path != d.store.Path {
		d.dropJournal()
		return r, nil
	}
	disk, _, err := d.store.read()
	if err != nil {
		return r, err
	}
	switch {
	case slices.Contains(j.Shown, hash(disk)) && !bytes.Equal(disk, j.Saved):
		if err := d.put(j.Saved, j.Existed); err != nil {
			return r, err
		}
		r.Repaired = true
	case bytes.Equal(disk, j.Saved):
	default:
		d.dropJournal()
		r.Dropped = len(j.Staged) > 0
		return r, nil
	}
	r.Staged = j.Staged
	return r, nil
}

// Package state keeps the TUI's own files: the change history behind
// "recent" and undo, and the journal of unsaved changes. They live in the
// TUI's state directory, never in Ghostty's config directories.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Entry is one change to one key.
type Entry struct {
	Time   time.Time `json:"time"`
	Path   string    `json:"path"`
	Key    string    `json:"key"`
	Before []string  `json:"before"`
	After  []string  `json:"after"`
}

// History is the persisted log of saved changes, oldest first.
type History struct {
	file    string
	Entries []Entry
}

const keep = 200

// Dir returns the state directory: $GHOSTUI_STATE_DIR, else
// $XDG_STATE_HOME/ghostui, else ~/.local/state/ghostui.
func Dir() string {
	if d := os.Getenv("GHOSTUI_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "ghostui")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "ghostui")
	}
	return filepath.Join(home, ".local", "state", "ghostui")
}

// Journal returns where the unsaved changes to the config at path are kept.
func Journal(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(Dir(), "unsaved-"+hex.EncodeToString(sum[:8])+".json")
}

// Open loads the history; a missing or unreadable file is an empty history.
func Open() *History {
	h := &History{file: filepath.Join(Dir(), "history.json")}
	h.load()
	return h
}

func (h *History) load() {
	h.Entries = nil
	if b, err := os.ReadFile(h.file); err == nil {
		_ = json.Unmarshal(b, &h.Entries)
	}
}

// Add returns entries plus a change. A change to the same key within a few
// seconds of the previous one (a burst of arrow presses) extends that entry
// instead, and an entry that ends where it started is dropped. entries itself
// is never modified, so callers can keep earlier versions.
func Add(entries []Entry, path, key string, before, after []string, now time.Time) []Entry {
	if slices.Equal(before, after) {
		return entries
	}
	if n := len(entries); n > 0 {
		last := entries[n-1]
		if last.Path == path && last.Key == key && now.Sub(last.Time) < 4*time.Second && slices.Equal(last.After, before) {
			last.After, last.Time = after, now
			if slices.Equal(last.Before, last.After) {
				return entries[:n-1]
			}
			return append(slices.Clone(entries[:n-1]), last)
		}
	}
	return append(slices.Clip(entries), Entry{Time: now, Path: path, Key: key, Before: before, After: after})
}

// For returns path's entries, oldest first.
func (h *History) For(path string) []Entry {
	var out []Entry
	for _, e := range h.Entries {
		if e.Path == path {
			out = append(out, e)
		}
	}
	return out
}

// Replace makes entries path's history and saves it. The file is read again
// first, so entries another instance wrote for other files survive. The
// oldest entries go once there are more than the history keeps.
func (h *History) Replace(path string, entries []Entry) {
	h.load()
	h.Entries = slices.DeleteFunc(h.Entries, func(e Entry) bool { return e.Path == path })
	h.Entries = append(h.Entries, entries...)
	slices.SortStableFunc(h.Entries, func(a, b Entry) int { return a.Time.Compare(b.Time) })
	if len(h.Entries) > keep {
		h.Entries = h.Entries[len(h.Entries)-keep:]
	}
	h.save()
}

func (h *History) save() {
	if err := os.MkdirAll(filepath.Dir(h.file), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(h.Entries, "", " ")
	if err != nil {
		return
	}
	tmp := h.file + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, h.file)
	}
}

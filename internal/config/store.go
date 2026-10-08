package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Validator checks a config file and returns Ghostty's problem lines.
type Validator func(ctx context.Context, path string) ([]string, error)

// Store reads and writes one config file.
type Store struct {
	Path     string
	Validate Validator
}

// Snapshot is the file as last read, with its modification time so callers
// can notice edits made outside the TUI.
type Snapshot struct {
	File    *File
	ModTime time.Time
	Size    int64
	Exists  bool
}

// Load reads the file. A missing file is an empty config.
func (s *Store) Load() (Snapshot, error) {
	b, exists, err := s.read()
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{File: Parse(b), Exists: exists}
	snap.ModTime, snap.Size = s.Stat()
	return snap, nil
}

// Stat returns the current modification time and size.
func (s *Store) Stat() (time.Time, int64) {
	fi, err := os.Stat(s.Path)
	if err != nil {
		return time.Time{}, 0
	}
	return fi.ModTime(), fi.Size()
}

// read returns the file's bytes; a missing file reads as empty.
func (s *Store) read() ([]byte, bool, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// real is the file behind the path: writes replace the symlink's target,
// never the symlink.
func (s *Store) real() string {
	if r, err := filepath.EvalSymlinks(s.Path); err == nil {
		return r
	}
	return s.Path
}

// Dir is the directory of the file behind the path, which relative
// config-file paths resolve against.
func (s *Store) Dir() string { return filepath.Dir(s.real()) }

// temp writes data to a temporary file next to the real one (so relative
// config-file includes still resolve) with the real file's mode.
func (s *Store) temp(data []byte) (string, error) {
	real := s.real()
	dir := filepath.Dir(real)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", readOnly(dir, err)
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(real); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(real)+".ghostui-*")
	if err != nil {
		return "", readOnly(dir, err)
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// readOnly explains a failure to write into dir that comes from the folder
// itself (say, a config managed by Nix): every check and save writes there.
func readOnly(dir string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) && (errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS)) {
		return fmt.Errorf("can't write to %s (%v): ghostui checks and saves the config by writing next to it", dir, pe.Err)
	}
	return err
}

// write atomically replaces the file with data.
func (s *Store) write(data []byte) error {
	tmp, err := s.temp(data)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, s.real()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// problems validates data as it would be written.
func (s *Store) problems(ctx context.Context, data []byte) ([]string, error) {
	tmp, err := s.temp(data)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	return s.Validate(ctx, tmp)
}

// Include is what a file pulled in with config-file sets for one key.
type Include struct {
	Path   string   // the last included file that sets the key
	Values []string // its values there
}

// Includes follows f's config-file entries, and those of the files they
// include, and returns what they set, by key. Ghostty loads an included file
// after the file that includes it, so for a key that isn't repeatable the
// included value is the one that applies. Relative paths resolve against
// the including file's directory, as validation does; files that can't be
// read are skipped.
func (s *Store) Includes(f *File) map[string]Include {
	out := map[string]Include{}
	type file struct {
		dir string
		f   *File
	}
	seen := map[string]bool{s.real(): true}
	queue := []file{{s.Dir(), f}}
	for len(queue) > 0 && len(seen) <= maxIncludes {
		cur := queue[0]
		queue = queue[1:]
		for _, v := range cur.f.Values("config-file") {
			p := strings.TrimPrefix(v, "?")
			if p == "" {
				continue
			}
			if rest, ok := strings.CutPrefix(p, "~/"); ok {
				if home, err := os.UserHomeDir(); err == nil {
					p = filepath.Join(home, rest)
				}
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(cur.dir, p)
			}
			if r, err := filepath.EvalSymlinks(p); err == nil {
				p = r
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			inc := Parse(b)
			for _, k := range inc.Keys() {
				if k != "config-file" {
					out[k] = Include{Path: p, Values: inc.Values(k)}
				}
			}
			queue = append(queue, file{filepath.Dir(p), inc})
		}
	}
	return out
}

// maxIncludes bounds how many files Includes reads.
const maxIncludes = 64

// Change sets key's values; see File.Replace for From.
type Change struct {
	Key      string
	From, To []string
}

// InvalidError is returned when a change would introduce config problems.
type InvalidError struct{ Problems []string }

func (e *InvalidError) Error() string {
	if len(e.Problems) == 1 {
		return e.Problems[0]
	}
	return fmt.Sprintf("%s (+%d more)", e.Problems[0], len(e.Problems)-1)
}

var reProblemLoc = regexp.MustCompile(`^.*?:\d+:`)

// Introduced returns the problems in next that aren't in prev, comparing
// them without their path:line: prefix.
func Introduced(prev, next []string) []string {
	var old []string
	for _, p := range prev {
		old = append(old, StripLocation(p))
	}
	var out []string
	for _, p := range next {
		if q := StripLocation(p); !slices.Contains(old, q) {
			out = append(out, q)
		}
	}
	return out
}

// StripLocation removes Ghostty's "path:line:" prefix from a problem.
func StripLocation(p string) string { return reProblemLoc.ReplaceAllString(p, "") }

// Package config edits a Ghostty config file in place. Comments, blank lines,
// ordering, spacing, quoting and repeated keys all survive a round trip; only
// the entries being changed are touched.
package config

import (
	"regexp"
	"slices"
	"strings"
)

// Line is one line of the file.
type Line struct {
	Raw string
	// Set for `key = value` entries.
	Key    string
	prefix string // indentation, key and separator, e.g. "font-size = "
	value  string // raw value text as written
	suffix string // trailing whitespace
}

// IsEntry reports whether the line is a key/value entry.
func (l Line) IsEntry() bool { return l.Key != "" }

// Value is the entry's value with surrounding quotes removed.
func (l Line) Value() string { return Unquote(l.value) }

// File is a parsed config file.
type File struct {
	Lines   []Line
	finalNL bool
	bom     bool // a UTF-8 byte order mark, kept on the way out
}

const bom = "\ufeff"

var reEntry = regexp.MustCompile(`^(\s*([A-Za-z0-9_-]+)\s*=\s*)(.*?)(\s*)$`)

// Parse parses config file contents. It never fails: lines that aren't
// entries are kept verbatim.
func Parse(b []byte) *File {
	s := string(b)
	f := &File{finalNL: strings.HasSuffix(s, "\n")}
	s, f.bom = strings.CutPrefix(s, bom)
	s = strings.TrimSuffix(s, "\n")
	if s == "" && !f.finalNL {
		return f
	}
	for _, raw := range strings.Split(s, "\n") {
		f.Lines = append(f.Lines, parseLine(raw))
	}
	return f
}

func parseLine(raw string) Line {
	l := Line{Raw: raw}
	t := strings.TrimSpace(raw)
	if t == "" || strings.HasPrefix(t, "#") {
		return l
	}
	if m := reEntry.FindStringSubmatch(raw); m != nil {
		l.prefix, l.Key, l.value, l.suffix = m[1], m[2], m[3], m[4]
	}
	return l
}

// Bytes renders the file.
func (f *File) Bytes() []byte {
	var b strings.Builder
	if f.bom {
		b.WriteString(bom)
	}
	for i, l := range f.Lines {
		b.WriteString(l.Raw)
		if i < len(f.Lines)-1 || f.finalNL {
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// Clone returns a deep copy.
func (f *File) Clone() *File {
	return &File{Lines: slices.Clone(f.Lines), finalNL: f.finalNL, bom: f.bom}
}

// Values returns the unquoted values of every entry for key, in file order.
func (f *File) Values(key string) []string {
	var vals []string
	for _, l := range f.Lines {
		if l.Key == key {
			vals = append(vals, l.Value())
		}
	}
	return vals
}

// Entries returns the line indexes (0-based) of key's entries.
func (f *File) Entries(key string) []int {
	var idx []int
	for i, l := range f.Lines {
		if l.Key == key {
			idx = append(idx, i)
		}
	}
	return idx
}

// Keys returns the distinct keys set in the file, in order of first use.
func (f *File) Keys() []string {
	var keys []string
	for _, l := range f.Lines {
		if l.IsEntry() && !slices.Contains(keys, l.Key) {
			keys = append(keys, l.Key)
		}
	}
	return keys
}

// Replace changes key's entries from the values in from to the values in to.
//
// A nil from means "whatever the file has now": to is then the complete,
// ordered list of values (a set, or an unset when to is empty). Otherwise
// from and to describe a change; if the file still holds exactly from, the
// result is exactly to (undo/redo), else the change is patched onto what's
// there: values only in from are replaced by, or removed for, values only in
// to.
//
// Order is kept, which matters for repeatable keys (the first font-family is
// the primary). Lines whose value survives are left alone, changed values
// are rewritten in place, and new lines go where the order puts them.
func (f *File) Replace(key string, from, to []string) {
	f.setEntries(key, Patch(f.Values(key), from, to))
}

// Patch returns cur with the change from → to applied, the way Replace
// applies it to a key's values.
func Patch(cur, from, to []string) []string {
	if from == nil || slices.Equal(cur, from) {
		return to
	}
	return patch(cur, from, to)
}

// patch applies the change from → to onto cur, as values: the values only
// in from are replaced, in order, by those only in to (or removed when to
// has fewer), and the rest of to's are added at the end. A value of from that
// cur no longer has was changed since; that newer change wins, so its
// replacement is dropped rather than added.
func patch(cur, from, to []string) []string {
	removed, added := diff(from, to)
	out := slices.Clone(cur)
	for i, v := range removed {
		j := slices.Index(out, v)
		if j < 0 {
			continue
		}
		var repl []string
		if i < len(added) {
			repl = added[i : i+1]
		}
		out = slices.Replace(out, j, j+1, repl...)
	}
	if len(added) > len(removed) {
		out = append(out, added[len(removed):]...)
	}
	return out
}

// setEntries makes key's entries hold exactly target, in order, with as few
// line edits as possible: entries on a longest common subsequence of the
// current and target values stay untouched, and in each gap between them
// unmatched lines are rewritten in place before any are deleted or inserted.
func (f *File) setEntries(key string, target []string) {
	idx := f.Entries(key)
	if len(idx) == 0 {
		f.appendEntries(key, target)
		return
	}
	cur := make([]string, len(idx))
	quoted := false
	for i, li := range idx {
		cur[i] = f.Lines[li].Value()
		quoted = quoted || strings.HasPrefix(f.Lines[li].value, `"`)
	}

	n, m := len(cur), len(target)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if cur[i] == target[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	rewrite := map[int]string{}  // line → new value
	drop := map[int]bool{}       // lines to delete
	after := map[int][]string{}  // new values to insert after a line
	before := map[int][]string{} // new values to insert before a line
	var gapCur []int             // unmatched current entries in this gap
	var gapNew []string          // unmatched target values in this gap
	prev := -1                   // line of the last matched entry
	closeGap := func(next int) { // next: line of the following match, or -1
		k := min(len(gapCur), len(gapNew))
		for x := 0; x < k; x++ {
			rewrite[idx[gapCur[x]]] = gapNew[x]
		}
		for _, c := range gapCur[k:] {
			drop[idx[c]] = true
		}
		if rest := gapNew[k:]; len(rest) > 0 {
			// Prefer sitting right above the next kept entry: a comment
			// above a group of entries stays above it.
			switch {
			case k > 0:
				after[idx[gapCur[k-1]]] = append(after[idx[gapCur[k-1]]], rest...)
			case next >= 0:
				before[next] = append(before[next], rest...)
			case prev >= 0:
				after[prev] = append(after[prev], rest...)
			default:
				// Nothing kept or reused: put them where the first entry was.
				before[idx[0]] = append(before[idx[0]], rest...)
			}
		}
		gapCur, gapNew = nil, nil
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case cur[i] == target[j]:
			closeGap(idx[i])
			prev = idx[i]
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			gapCur = append(gapCur, i)
			i++
		default:
			gapNew = append(gapNew, target[j])
			j++
		}
	}
	for ; i < n; i++ {
		gapCur = append(gapCur, i)
	}
	gapNew = append(gapNew, target[j:]...)
	closeGap(-1)

	entries := func(vals []string) []Line {
		var ls []Line
		for _, v := range vals {
			ls = append(ls, parseLine(key+" = "+format(v, quoted)))
		}
		return ls
	}
	var out []Line
	for li, l := range f.Lines {
		out = append(out, entries(before[li])...)
		switch v, ok := rewrite[li]; {
		case ok:
			out = append(out, l.with(v))
		case !drop[li]:
			out = append(out, l)
		}
		out = append(out, entries(after[li])...)
	}
	f.Lines = out
}

func (f *File) appendEntries(key string, vals []string) {
	if len(vals) == 0 {
		return
	}
	// Don't append after trailing blank lines; keep them at the end.
	at := len(f.Lines)
	for at > 0 && strings.TrimSpace(f.Lines[at-1].Raw) == "" {
		at--
	}
	var lines []Line
	for _, v := range vals {
		lines = append(lines, newEntry(key, v))
	}
	f.Lines = slices.Insert(f.Lines, at, lines...)
	f.finalNL = true
}

// LineOf returns the 1-based line number of the entry for key with value v,
// or 0.
func (f *File) LineOf(key, v string) int {
	for i, l := range f.Lines {
		if l.Key == key && l.Value() == v {
			return i + 1
		}
	}
	return 0
}

func (l Line) with(v string) Line {
	quoted := strings.HasPrefix(l.value, `"`)
	nl := l
	nl.value = format(v, quoted)
	nl.Raw = nl.prefix + nl.value + nl.suffix
	return nl
}

func newEntry(key, v string) Line {
	return parseLine(key + " = " + format(v, false))
}

// format quotes v when the original was quoted or when Ghostty would
// otherwise lose information (leading/trailing space, empty reset values).
func format(v string, quoted bool) string {
	if quoted || v == "" || strings.TrimSpace(v) != v {
		return `"` + v + `"`
	}
	return v
}

// Unquote strips one pair of surrounding double quotes.
func Unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

// diff returns the multiset differences a−b and b−a, preserving order.
func diff(a, b []string) (onlyA, onlyB []string) {
	count := map[string]int{}
	for _, v := range b {
		count[v]++
	}
	for _, v := range a {
		if count[v] > 0 {
			count[v]--
		} else {
			onlyA = append(onlyA, v)
		}
	}
	count = map[string]int{}
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		if count[v] > 0 {
			count[v]--
		} else {
			onlyB = append(onlyB, v)
		}
	}
	return onlyA, onlyB
}

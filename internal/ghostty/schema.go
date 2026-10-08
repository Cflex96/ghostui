package ghostty

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Kind is the value type of an option, inferred from Ghostty itself.
type Kind int

const (
	KString Kind = iota
	KBool
	KInt
	KFloat
	KColor
	KDuration
	KEnum
	KFlags
	KFont
	KTheme
	KKeybind
	KPalette
)

func (k Kind) String() string {
	return [...]string{"string", "bool", "int", "float", "color", "duration", "enum",
		"flags", "font", "theme", "keybind", "palette"}[k]
}

// Numeric reports whether values of this kind are numbers.
func (k Kind) Numeric() bool { return k == KInt || k == KFloat }

// Option describes one config key.
type Option struct {
	Key        string
	Kind       Kind
	Defaults   []string // as printed by +show-config --default; "" means unset
	Docs       string   // markdown-ish docs with the leading "# " removed
	Values     []string // enum values (strict) or suggestions for strings
	Flags      []string // field names of a packed-flags option
	Min, Max   float64
	HasRange   bool
	Repeatable bool
	Platform   string // "", "macos" or "linux"
	Unit       string // "pt", "px", "ms", "cells" when the docs say so
	Category   string
	Related    []string
	Index      int
}

// Default returns the single default value ("" when unset).
func (o *Option) Default() string {
	if o.Kind == KKeybind || o.Kind == KPalette || len(o.Defaults) == 0 {
		return ""
	}
	return o.Defaults[len(o.Defaults)-1]
}

// Label is the short type/unit tag shown next to a value.
func (o *Option) Label() string {
	if o.Unit != "" {
		return o.Unit
	}
	return o.Kind.String()
}

// Summary returns the first paragraph of the docs as one line.
func (o *Option) Summary() string { return Paragraphs(o.Docs, 1) }

// Paragraphs returns the first n prose paragraphs of docs, unwrapped. Code
// blocks (indented lines) and bullet lists are skipped.
func Paragraphs(docs string, n int) string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, l := range strings.Split(docs, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
		case strings.HasPrefix(l, "    "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "- "):
			flush()
		default:
			cur = append(cur, t)
		}
		if len(paras) >= n {
			break
		}
	}
	flush()
	if len(paras) > n {
		paras = paras[:n]
	}
	return strings.ReplaceAll(strings.Join(paras, "\n\n"), "`", "")
}

// Schema is every option Ghostty knows, in Ghostty's documentation order.
type Schema struct {
	Version string
	Options []*Option
	byKey   map[string]*Option
}

// Get returns the option for key, or nil.
func (s *Schema) Get(key string) *Option {
	if s == nil {
		return nil
	}
	return s.byKey[key]
}

// LoadSchema reads options, defaults and docs from `+show-config --default
// --docs`, then infers each option's type by probing `+validate-config`.
func LoadSchema(ctx context.Context, c *CLI) (*Schema, error) {
	out, errOut, err := c.Run(ctx, "+show-config", "--default", "--docs")
	if err != nil {
		return nil, fmt.Errorf("ghostty +show-config: %v %s", err, strings.TrimSpace(errOut))
	}
	opts := parseShowConfig(out)
	if len(opts) == 0 {
		return nil, fmt.Errorf("ghostty +show-config printed no options")
	}
	probes, perr := probe(ctx, c, opts)
	classify(opts, probes, perr == nil)
	s := &Schema{Version: c.Version(ctx), Options: opts, byKey: map[string]*Option{}}
	for i, o := range opts {
		o.Index = i
		s.byKey[o.Key] = o
	}
	for _, o := range opts {
		o.Related = related(s, o)
	}
	return s, nil
}

// parseShowConfig parses `+show-config --default --docs` output. Docs are the
// "# " lines right before a key. Keys printed without docs (font-family-bold
// after font-family, ...) share the previous key's docs.
func parseShowConfig(text string) []*Option {
	var opts []*Option
	byKey := map[string]*Option{}
	var docBuf []string
	lastDocs := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			s := strings.TrimPrefix(line, "#")
			docBuf = append(docBuf, strings.TrimPrefix(s, " "))
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimPrefix(val, " ")
		if o := byKey[key]; o != nil {
			o.Defaults = append(o.Defaults, val)
			docBuf = nil
			continue
		}
		docs := strings.TrimRight(strings.Join(docBuf, "\n"), "\n ")
		if strings.TrimSpace(docs) == "" {
			docs = lastDocs
		}
		lastDocs = docs
		docBuf = nil
		o := &Option{Key: key, Defaults: []string{val}, Docs: docs}
		byKey[key] = o
		opts = append(opts, o)
	}
	return opts
}

// probeValues are written for every key at once, one validate run per value.
// Which values a key accepts identifies its type; Ghostty's error for the
// first one lists the valid values of enums.
var probeValues = [...]string{"__ghostui_probe__", "true", "7", "0.5", "#123456", "5s"}

const (
	pAny = iota
	pBool
	pInt
	pFloat
	pColor
	pDuration
)

type probeResult struct {
	accepts [len(probeValues)]bool
	enum    []string
}

var (
	reProblem   = regexp.MustCompile(`:(\d+):([a-z0-9-]+): (.*)$`)
	reValidVals = regexp.MustCompile(`valid values are: (.*)$`)
)

func probe(ctx context.Context, c *CLI, opts []*Option) (map[string]*probeResult, error) {
	dir, err := os.MkdirTemp("", "ghostui-probe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	res := make(map[string]*probeResult, len(opts))
	for _, o := range opts {
		r := &probeResult{}
		for i := range r.accepts {
			r.accepts[i] = true
		}
		res[o.Key] = r
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for pi, pv := range probeValues {
		var b strings.Builder
		for _, o := range opts {
			fmt.Fprintf(&b, "%s = %s\n", o.Key, pv)
		}
		path := filepath.Join(dir, fmt.Sprintf("probe-%d", pi))
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			problems, err := c.Validate(ctx, path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				firstErr = err
				return
			}
			for _, p := range problems {
				m := reProblem.FindStringSubmatch(p)
				if m == nil {
					continue
				}
				n, _ := strconv.Atoi(m[1])
				if n < 1 || n > len(opts) || opts[n-1].Key != m[2] {
					continue
				}
				r := res[m[2]]
				r.accepts[pi] = false
				if pi == pAny {
					if vm := reValidVals.FindStringSubmatch(m[3]); vm != nil {
						r.enum = strings.Split(vm[1], ", ")
					}
				}
			}
		}()
	}
	wg.Wait()
	return res, firstErr
}

var (
	reDocValue   = regexp.MustCompile("(?m)^\\s*\\*\\s+`([^`]+)`")
	reRepeatable = regexp.MustCompile(`(?i)(can|may) be (repeated|specified multiple times|used multiple times)|repeated multiple times`)
	rePlatform   = regexp.MustCompile(`(?m)^(macOS|GTK|Linux) only\.?$`)
	num          = `(-?\d+(?:\.\d+)?)`
	reRanges     = []*regexp.Regexp{
		regexp.MustCompile(`less than ` + num + ` or greater than ` + num),
		regexp.MustCompile(`between ` + num + ` and ` + num),
		regexp.MustCompile(`range (?:of )?` + num + ` to ` + num),
		regexp.MustCompile(`ranging from ` + num + ` to ` + num),
		regexp.MustCompile(`from ` + num + ` \([^)]*\) to ` + num),
	}
)

func classify(opts []*Option, probes map[string]*probeResult, probed bool) {
	for _, o := range opts {
		o.Repeatable = len(o.Defaults) > 1 || reRepeatable.MatchString(o.Docs)
		o.Platform = platform(o)
		o.Category = category(o.Key)
		def := o.Defaults[0]
		docVals := docValues(o.Docs)

		switch {
		case o.Key == "theme":
			o.Kind = KTheme
		case o.Key == "keybind":
			o.Kind = KKeybind
		case o.Key == "palette":
			o.Kind = KPalette
		case strings.HasPrefix(o.Key, "font-family") || strings.HasSuffix(o.Key, "-font-family"):
			o.Kind = KFont
		case !probed:
			o.Kind = guessKind(def)
		default:
			r := probes[o.Key]
			a := r.accepts
			switch {
			case len(r.enum) > 0:
				o.Kind, o.Values = KEnum, r.enum
			case a[pAny]:
				o.Kind = KString
				if len(docVals) > 1 {
					o.Values = docVals
				}
			case a[pBool] && a[pInt]:
				// e.g. background-blur: a bool, an intensity or macOS glass styles.
				o.Kind = KString
				o.Values = docVals
			case a[pBool] && (strings.Contains(def, ",") || (def != "" && def != "true" && def != "false")):
				o.Kind = KFlags
				for _, f := range strings.Split(def, ",") {
					o.Flags = append(o.Flags, strings.TrimPrefix(f, "no-"))
				}
			case a[pBool]:
				o.Kind = KBool
			case a[pInt] && a[pFloat] && !strings.Contains(def, ":"):
				o.Kind = KFloat
			case a[pInt] && !a[pFloat]:
				o.Kind = KInt
			case a[pColor]:
				o.Kind = KColor
			case a[pDuration]:
				o.Kind = KDuration
			default:
				o.Kind = KString
				if len(docVals) > 1 {
					o.Values = docVals
				}
			}
		}
		if o.Kind.Numeric() {
			o.Min, o.Max, o.HasRange = numRange(o)
			o.Unit = unit(o.Docs)
		}
	}
}

// guessKind is the fallback when validate-config can't be probed.
func guessKind(def string) Kind {
	switch {
	case def == "true" || def == "false":
		return KBool
	case strings.HasPrefix(def, "#") && len(def) == 7:
		return KColor
	}
	if _, err := strconv.Atoi(def); err == nil {
		return KInt
	}
	if _, err := strconv.ParseFloat(def, 64); err == nil {
		return KFloat
	}
	return KString
}

func docValues(docs string) []string {
	var vals []string
	for _, m := range reDocValue.FindAllStringSubmatch(docs, -1) {
		if v := m[1]; !strings.ContainsAny(v, " =") && !slices.Contains(vals, v) {
			vals = append(vals, v)
		}
	}
	return vals
}

func numRange(o *Option) (float64, float64, bool) {
	docs := strings.ReplaceAll(strings.ReplaceAll(o.Docs, "\n", " "), "`", "")
	def, defErr := strconv.ParseFloat(o.Default(), 64)
	for _, re := range reRanges {
		m := re.FindStringSubmatch(docs)
		if m == nil {
			continue
		}
		lo, err1 := strconv.ParseFloat(m[1], 64)
		hi, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil || lo >= hi {
			continue
		}
		if defErr == nil && (def < lo || def > hi) {
			continue
		}
		return lo, hi, true
	}
	if o.Kind == KFloat && strings.Contains(o.Key, "opacity") {
		return 0, 1, true
	}
	return 0, 0, false
}

func unit(docs string) string {
	d := strings.ToLower(strings.ReplaceAll(docs, "\n", " "))
	switch {
	case strings.Contains(d, "in points"), strings.Contains(d, "size in points"):
		return "pt"
	case strings.Contains(d, "in pixels"):
		return "px"
	case strings.Contains(d, "milliseconds"):
		return "ms"
	case strings.Contains(d, "grid cells"):
		return "cells"
	}
	return ""
}

func platform(o *Option) string {
	k := o.Key
	switch {
	case strings.HasPrefix(k, "macos-"):
		return "macos"
	case strings.HasPrefix(k, "gtk-"), strings.HasPrefix(k, "linux-"), strings.HasPrefix(k, "x11-"):
		return "linux"
	}
	if m := rePlatform.FindStringSubmatch(o.Docs); m != nil {
		if m[1] == "macOS" {
			return "macos"
		}
		return "linux"
	}
	return ""
}

// Irrelevant reports whether the option has no effect on this OS.
func (o *Option) Irrelevant() bool {
	switch o.Platform {
	case "macos":
		return runtime.GOOS != "darwin"
	case "linux":
		return runtime.GOOS == "darwin"
	}
	return false
}

// Categories are the browsable tabs that list options (keys and themes have
// their own screens).
var Categories = []string{"appearance", "font", "window", "clipboard", "more"}

func category(k string) string {
	has := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				return true
			}
		}
		return false
	}
	is := func(keys ...string) bool { return slices.Contains(keys, k) }
	switch {
	case is("keybind"):
		return "keys"
	case has("font-", "adjust-", "freetype-") || is("grapheme-width-method", "window-title-font-family"):
		return "font"
	case has("clipboard-", "selection-clear-") || is("copy-on-select", "selection-word-chars", "right-click-action"):
		return "clipboard"
	case is("theme", "foreground", "minimum-contrast", "bold-color", "faint-opacity", "split-divider-color",
		"alpha-blending", "window-colorspace", "window-theme", "selection-foreground", "selection-background") ||
		has("background", "palette", "cursor-", "unfocused-split-", "search-", "custom-shader", "macos-icon", "macos-custom-icon"):
		return "appearance"
	case has("window-", "macos-titlebar-", "macos-window-", "resize-overlay", "quick-terminal-", "gtk-quick-terminal-",
		"quit-after-last-window-closed", "gtk-titlebar", "gtk-tabs-", "gtk-wide-tabs", "gtk-toolbar-") ||
		is("macos-non-native-fullscreen", "maximize", "fullscreen", "title", "class", "x11-instance-name",
			"initial-window", "confirm-close-surface", "focus-follows-mouse", "tab-inherit-working-directory",
			"split-inherit-working-directory", "split-preserve-zoom", "macos-hidden", "macos-dock-drop-behavior",
			"undo-timeout", "gtk-single-instance"):
		return "window"
	}
	return "more"
}

var reBacktick = regexp.MustCompile("`([a-z][a-z0-9-]+)`")

// related lists options the docs point at, then close siblings by name.
func related(s *Schema, o *Option) []string {
	var out []string
	add := func(k string) {
		if k != o.Key && s.byKey[k] != nil && !slices.Contains(out, k) && len(out) < 3 {
			out = append(out, k)
		}
	}
	for _, m := range reBacktick.FindAllStringSubmatch(o.Docs, -1) {
		add(m[1])
	}
	for _, p := range s.Options {
		if p.Key != o.Key && (strings.HasPrefix(p.Key, o.Key+"-") || strings.HasPrefix(o.Key, p.Key+"-")) {
			add(p.Key)
		}
	}
	return out
}

// Step returns the arrow-key increment for a numeric option; fine is used
// with shift (finer for floats, ×10 for ints).
func (o *Option) Step(cur float64, fine bool) float64 {
	if o.Kind == KInt {
		step := 1.0
		if !o.HasRange && math.Abs(cur) >= 100 {
			step = math.Pow(10, math.Floor(math.Log10(math.Abs(cur)))-1)
		}
		if fine {
			step *= 10
		}
		return step
	}
	if o.HasRange {
		span := o.Max - o.Min
		step := math.Pow(10, math.Floor(math.Log10(span/10)))
		switch {
		case span/step > 40:
			step *= 5
		case span/step < 15:
			step /= 2
		}
		if fine {
			step /= 5
			if span <= 1 {
				step = 0.01
			}
		}
		return step
	}
	switch {
	case math.Abs(cur) < 1 && fine:
		return 0.01
	case math.Abs(cur) < 1:
		return 0.05
	case fine:
		return 0.5
	}
	return 1
}

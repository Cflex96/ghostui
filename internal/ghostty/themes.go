package ghostty

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Theme is one entry of `ghostty +list-themes` with its colors loaded.
type Theme struct {
	Name    string
	Source  string // "resources" or "user"
	Path    string
	Dark    bool
	BG, FG  string
	Cursor  string
	Palette [16]string
}

// Swatch returns the 8 colors of the theme strip: bg, red, green, yellow,
// blue, magenta, cyan, fg.
func (t *Theme) Swatch() []string {
	return []string{t.BG, t.Palette[1], t.Palette[2], t.Palette[3], t.Palette[4], t.Palette[5], t.Palette[6], t.FG}
}

var reThemeLine = regexp.MustCompile(`^(.+) \((resources|user)\) (/.+)$`)

// ListThemes lists the installed themes, classifies them as dark or light
// with `--color`, and reads each theme file's colors.
func ListThemes(ctx context.Context, c *CLI) ([]*Theme, error) {
	var all, dark string
	var errAll error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); all, _, errAll = c.Run(ctx, "+list-themes", "--plain", "--path") }()
	go func() { defer wg.Done(); dark, _, _ = c.Run(ctx, "+list-themes", "--plain", "--color=dark") }()
	wg.Wait()
	if errAll != nil {
		return nil, errAll
	}
	isDark := map[string]bool{}
	for _, l := range strings.Split(dark, "\n") {
		if m := reThemeLine.FindStringSubmatch(l + " /"); m != nil {
			isDark[m[1]] = true
		} else if name, _, ok := strings.Cut(l, " ("); ok {
			isDark[name] = true
		}
	}
	var themes []*Theme
	for _, l := range strings.Split(all, "\n") {
		m := reThemeLine.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		themes = append(themes, &Theme{Name: m[1], Source: m[2], Path: m[3]})
	}
	sem := make(chan struct{}, 16)
	for _, t := range themes {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			if b, err := os.ReadFile(t.Path); err == nil {
				t.parse(string(b))
			}
		}()
	}
	wg.Wait()
	for _, t := range themes {
		if len(isDark) > 0 {
			t.Dark = isDark[t.Name]
		} else {
			t.Dark = Luminance(t.BG) < 0.5
		}
	}
	return themes, nil
}

func (t *Theme) parse(s string) {
	t.BG, t.FG = "#282c34", "#ffffff"
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "background":
			t.BG = normHex(v)
		case "foreground":
			t.FG = normHex(v)
		case "cursor-color":
			t.Cursor = normHex(v)
		case "palette":
			i, c, ok := strings.Cut(v, "=")
			if n, err := strconv.Atoi(strings.TrimSpace(i)); ok && err == nil && n >= 0 && n < 16 {
				t.Palette[n] = normHex(strings.TrimSpace(c))
			}
		}
	}
	for i, c := range t.Palette {
		if c == "" {
			t.Palette[i] = t.FG
		}
	}
}

func normHex(v string) string {
	v = strings.Trim(v, `"`)
	if !strings.HasPrefix(v, "#") {
		v = "#" + v
	}
	return strings.ToLower(v)
}

// Luminance returns the relative luminance (0..1) of a #rrggbb color.
func Luminance(hex string) float64 {
	r, g, b, ok := ParseHex(hex)
	if !ok {
		return 0
	}
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
}

// ParseHex parses #rgb or #rrggbb.
func ParseHex(hex string) (r, g, b uint8, ok bool) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

// ThemeValue is the parsed `theme` option: one theme, or a light/dark pair.
type ThemeValue struct {
	Light, Dark string
}

// ParseThemeValue parses "Name" or "light:A,dark:B".
func ParseThemeValue(v string) ThemeValue {
	v = strings.Trim(strings.TrimSpace(v), `"`)
	if !strings.Contains(v, "light:") && !strings.Contains(v, "dark:") {
		return ThemeValue{Light: v, Dark: v}
	}
	var tv ThemeValue
	for _, part := range strings.Split(v, ",") {
		k, name, _ := strings.Cut(strings.TrimSpace(part), ":")
		switch k {
		case "light":
			tv.Light = strings.TrimSpace(name)
		case "dark":
			tv.Dark = strings.TrimSpace(name)
		}
	}
	return tv
}

// Pair reports whether light and dark differ.
func (t ThemeValue) Pair() bool { return t.Light != t.Dark }

func (t ThemeValue) String() string {
	if !t.Pair() {
		return t.Dark
	}
	return "light:" + t.Light + ",dark:" + t.Dark
}

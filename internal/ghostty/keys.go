package ghostty

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ListFonts returns the font family names from `ghostty +list-fonts`.
func ListFonts(ctx context.Context, c *CLI) ([]string, error) {
	out, _, err := c.Run(ctx, "+list-fonts")
	if err != nil {
		return nil, err
	}
	var fams []string
	for _, l := range strings.Split(out, "\n") {
		if l == "" || l[0] == ' ' || l[0] == '\t' {
			continue
		}
		if name := strings.TrimSpace(l); !slices.Contains(fams, name) {
			fams = append(fams, name)
		}
	}
	return fams, nil
}

// Action is a bindable action, possibly with a parameter (goto_split:right).
type Action struct {
	Name string
	Desc string
}

var rePaletteEntry = regexp.MustCompile(`description:"([^"]*)",action:"([^"]*)"`)

// ListActions merges `+list-actions --docs` with the parameterized actions
// that appear in the default keybinds and command palette, so the recorder
// can offer goto_split:right rather than just goto_split.
func ListActions(ctx context.Context, c *CLI, s *Schema) ([]Action, error) {
	out, _, err := c.Run(ctx, "+list-actions", "--docs")
	if err != nil {
		return nil, err
	}
	desc := map[string]string{}
	var order []string
	var cur string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l != "" && l[0] != ' ' && strings.HasSuffix(l, ":"):
			cur = strings.TrimSuffix(l, ":")
			order = append(order, cur)
		case cur != "" && desc[cur] == "" && strings.TrimSpace(l) != "":
			desc[cur] = strings.TrimSpace(l)
		}
	}
	paramDesc := map[string]string{}
	if o := s.Get("command-palette-entry"); o != nil {
		for _, d := range o.Defaults {
			if m := rePaletteEntry.FindStringSubmatch(d); m != nil {
				paramDesc[m[2]] = m[1]
			}
		}
	}
	if o := s.Get("keybind"); o != nil {
		for _, d := range o.Defaults {
			if b, ok := ParseBinding(d); ok {
				if _, seen := paramDesc[b.Action]; !seen {
					paramDesc[b.Action] = ""
				}
			}
		}
	}
	var acts []Action
	seen := map[string]bool{}
	for _, name := range order {
		var variants []string
		for a := range paramDesc {
			if strings.HasPrefix(a, name+":") {
				variants = append(variants, a)
			}
		}
		sort.Strings(variants)
		if len(variants) == 0 || paramDesc[name] != "" {
			acts = append(acts, Action{Name: name, Desc: firstNonEmpty(paramDesc[name], desc[name])})
			seen[name] = true
		}
		for _, v := range variants {
			if !seen[v] {
				acts = append(acts, Action{Name: v, Desc: firstNonEmpty(paramDesc[v], desc[name])})
				seen[v] = true
			}
		}
	}
	return acts, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return strings.TrimSuffix(v, ".")
		}
	}
	return ""
}

// Binding is one `keybind = trigger=action` value.
type Binding struct {
	Prefixes []string // global, all, unconsumed, performable
	Trigger  string
	Action   string
}

var bindPrefixes = []string{"global", "all", "unconsumed", "performable"}

// ParseBinding splits "global:ctrl+a=action". The trigger can itself contain
// "=" (super+==increase_font_size:1), so the separator is the first "=" that
// leaves a complete trigger before it.
func ParseBinding(v string) (Binding, bool) {
	var b Binding
	rest := v
	for {
		p, after, ok := strings.Cut(rest, ":")
		if !ok || !slices.Contains(bindPrefixes, p) {
			break
		}
		b.Prefixes = append(b.Prefixes, p)
		rest = after
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] != '=' {
			continue
		}
		t := rest[:i]
		if t == "" || strings.HasSuffix(t, "+") || strings.HasSuffix(t, ">") {
			continue
		}
		b.Trigger, b.Action = t, rest[i+1:]
		return b, b.Action != ""
	}
	return b, false
}

func (b Binding) String() string {
	var sb strings.Builder
	for _, p := range b.Prefixes {
		sb.WriteString(p + ":")
	}
	return sb.String() + b.Trigger + "=" + b.Action
}

// Global reports whether the binding has the global: prefix.
func (b Binding) Global() bool { return slices.Contains(b.Prefixes, "global") }

var modAlias = map[string]string{
	"cmd": "super", "command": "super", "super": "super",
	"ctrl": "ctrl", "control": "ctrl",
	"alt": "alt", "opt": "alt", "option": "alt",
	"shift": "shift",
}

// modOrder matches the order Ghostty prints triggers in.
var modOrder = []string{"super", "ctrl", "alt", "shift"}

// NormalizeTrigger puts modifiers in canonical order and resolves aliases so
// two spellings of the same chord compare equal.
func NormalizeTrigger(t string) string {
	steps := strings.Split(t, ">")
	for i, step := range steps {
		parts := splitChord(step)
		mods := map[string]bool{}
		key := ""
		for _, p := range parts {
			if m, ok := modAlias[strings.ToLower(p)]; ok {
				mods[m] = true
			} else {
				key = strings.ToLower(p)
			}
		}
		var out []string
		for _, m := range modOrder {
			if mods[m] {
				out = append(out, m)
			}
		}
		if key != "" {
			out = append(out, key)
		}
		steps[i] = strings.Join(out, "+")
	}
	return strings.Join(steps, ">")
}

// ChordParts splits one chord into its modifiers and key, e.g. ctrl+alt+g.
func ChordParts(t string) []string { return splitChord(t) }

// splitChord splits on "+", keeping a literal "+" key (super++).
func splitChord(s string) []string {
	var parts []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '+' && cur != "" {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	return parts
}

// IsModifier reports whether a chord part is a modifier name.
func IsModifier(p string) bool {
	_, ok := modAlias[strings.ToLower(p)]
	return ok
}

// ActionGroup buckets an action for the grouped keybind list.
func ActionGroup(b Binding) string {
	a := b.Action
	switch {
	case b.Global():
		return "global"
	case strings.Contains(a, "split"):
		return "splits"
	case strings.Contains(a, "tab"):
		return "tabs"
	case strings.Contains(a, "window"), strings.Contains(a, "fullscreen"), strings.Contains(a, "maximize"), strings.Contains(a, "quick_terminal"):
		return "windows"
	case strings.Contains(a, "clipboard"), strings.Contains(a, "copy"), strings.Contains(a, "paste"):
		return "clipboard"
	case strings.Contains(a, "font_size"):
		return "font"
	case strings.Contains(a, "scroll"), strings.Contains(a, "jump_to_prompt"):
		return "scrolling"
	}
	return "other"
}

// GroupOrder is the display order of ActionGroup buckets.
var GroupOrder = []string{"splits", "tabs", "windows", "global", "clipboard", "font", "scrolling", "other"}

package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// input is a one-line text field drawn with the accent block cursor.
type input struct {
	r   []rune
	pos int
}

func (in *input) Value() string { return string(in.r) }

func (in *input) Set(s string) {
	in.r = []rune(s)
	in.pos = len(in.r)
}

func (in *input) Insert(s string) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	rs := []rune(s)
	in.r = append(in.r[:in.pos], append(rs, in.r[in.pos:]...)...)
	in.pos += len(rs)
}

// Handle applies an editing key and reports whether it was one. With
// cursor false, left/right are left to the caller (the palette uses them to
// adjust values).
func (in *input) Handle(msg tea.KeyPressMsg, cursor bool) bool {
	k := msg.Key()
	switch msg.String() {
	case "backspace", "ctrl+h":
		if in.pos > 0 {
			in.r = append(in.r[:in.pos-1], in.r[in.pos:]...)
			in.pos--
		}
		return true
	case "delete", "ctrl+d":
		if cursor && in.pos < len(in.r) {
			in.r = append(in.r[:in.pos], in.r[in.pos+1:]...)
		}
		return cursor
	case "ctrl+w", "alt+backspace":
		i := in.pos
		for i > 0 && in.r[i-1] == ' ' {
			i--
		}
		for i > 0 && in.r[i-1] != ' ' {
			i--
		}
		in.r = append(in.r[:i], in.r[in.pos:]...)
		in.pos = i
		return true
	case "ctrl+u":
		in.r = in.r[in.pos:]
		in.pos = 0
		return true
	}
	if cursor {
		switch msg.String() {
		case "left", "ctrl+b":
			if in.pos > 0 {
				in.pos--
			}
			return true
		case "right", "ctrl+f":
			if in.pos < len(in.r) {
				in.pos++
			}
			return true
		case "home", "ctrl+a":
			in.pos = 0
			return true
		case "end", "ctrl+e":
			in.pos = len(in.r)
			return true
		}
	}
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		in.Insert(k.Text)
		return true
	}
	return false
}

// View renders the text with a cursor when focused, or the placeholder.
func (in *input) View(placeholder string, focused bool, w int) string {
	cur := sAccent.Render("█")
	if len(in.r) == 0 {
		if focused {
			return cur + sFaint.Render(trunc(placeholder, w-1))
		}
		return sFaint.Render(trunc(placeholder, w))
	}
	if !focused {
		return sMuted.Render(trunc(string(in.r), w))
	}
	before := string(in.r[:in.pos])
	if in.pos == len(in.r) {
		// Keep the end of long text visible.
		if width(before)+1 > w {
			rs := []rune(before)
			for len(rs) > 0 && width(string(rs))+2 > w {
				rs = rs[1:]
			}
			before = "…" + string(rs)
		}
		return sText.Render(before) + cur
	}
	at := string(in.r[in.pos])
	after := string(in.r[in.pos+1:])
	under := sPill.UnsetBold().Render(at)
	return trunc(sText.Render(before)+under+sText.Render(after), w)
}

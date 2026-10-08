package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func BenchmarkView(b *testing.B) {
	a, _ := newTestApp(b)
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	for _, c := range []struct {
		name string
		prep func()
	}{
		{"home", func() { a.ov = ovNone; a.setTab(tabOverview) }},
		{"palette", func() { a.setTab(tabOverview); a.openPalette("opac") }},
		{"themes", func() { a.ov = ovNone; a.setTab(tabThemes) }},
		{"keys", func() { a.ov = ovNone; a.setTab(tabKeys) }},
	} {
		c.prep()
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				_ = a.View()
			}
		})
	}
	b.Run("palette-keystroke", func(b *testing.B) {
		a.setTab(tabOverview)
		a.openPalette("")
		for b.Loop() {
			a.pal.q.Set("background op")
			a.pal.refresh(a)
			a.syncEditor()
			_ = a.View()
		}
	})
}

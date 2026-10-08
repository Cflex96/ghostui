package fuzzy

import (
	"slices"
	"testing"
)

func TestFind(t *testing.T) {
	m, ok := Find("opac", "background-opacity")
	if !ok || !slices.Equal(m.Indices, []int{11, 12, 13, 14}) {
		t.Errorf("opac: %v %v", m, ok)
	}
	m, ok = Find("goto spl", "goto_split:right")
	if !ok || !slices.Equal(m.Indices, []int{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("goto spl: %v %v", m, ok)
	}
	if _, ok := Find("xyz", "background-opacity"); ok {
		t.Error("xyz matched")
	}
	a, _ := Find("fs", "font-size")
	b, _ := Find("fs", "font-shaping-break")
	c, _ := Find("fs", "selection-foreground-something")
	if a.Score <= c.Score || b.Score <= c.Score {
		t.Errorf("boundary matches should win: %d %d %d", a.Score, b.Score, c.Score)
	}
	x, _ := Find("opacity", "background-opacity")
	y, _ := Find("opacity", "background-opacity-cells")
	if x.Score <= y.Score {
		t.Errorf("shorter target should win: %d %d", x.Score, y.Score)
	}
}

func TestWords(t *testing.T) {
	if !Words("blur glass", "Standard glass effect with blur") || Words("blur nope", "blur") || Words("", "x") {
		t.Error("Words")
	}
}

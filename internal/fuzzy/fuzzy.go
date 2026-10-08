// Package fuzzy is a small fzf-style subsequence matcher that reports which
// runes matched, for highlighting.
package fuzzy

import (
	"strings"
	"unicode"
)

// Match is a successful match.
type Match struct {
	Score   int
	Indices []int // rune indexes into the target
}

const (
	scoreMatch       = 16
	bonusConsecutive = 12
	bonusBoundary    = 10
	bonusFirst       = 8
	penaltyGapOpen   = 5
	penaltyGapChar   = 2
)

func isSep(r rune) bool { return r == '-' || r == '_' || r == ':' || r == ' ' || r == '.' || r == '/' }

// Find matches pattern against target, case-insensitively. A space in the
// pattern matches any separator, so "goto spl" matches "goto_split".
func Find(pattern, target string) (Match, bool) {
	p := []rune(strings.ToLower(strings.TrimSpace(pattern)))
	t := []rune(strings.ToLower(target))
	if len(p) == 0 {
		return Match{}, true
	}
	if len(p) > len(t) {
		return Match{}, false
	}
	eq := func(pr, tr rune) bool {
		if pr == ' ' {
			return isSep(tr)
		}
		return pr == tr
	}
	// Quick reject: p must be a subsequence of t.
	for i, j := 0, 0; i < len(p); j++ {
		if j == len(t) {
			return Match{}, false
		}
		if eq(p[i], t[j]) {
			i++
		}
	}

	orig := []rune(target)
	bonus := make([]int, len(t))
	for j := range t {
		switch {
		case j == 0:
			bonus[j] = bonusBoundary + bonusFirst
		case isSep(t[j-1]):
			bonus[j] = bonusBoundary
		case len(orig) == len(t) && unicode.IsLower(orig[j-1]) && unicode.IsUpper(orig[j]):
			bonus[j] = bonusBoundary
		}
	}

	const neg = -1 << 30
	n, m := len(p), len(t)
	dp := make([][]int, n)
	from := make([][]int, n) // previous match position, -1 for the first rune
	for i := range dp {
		dp[i] = make([]int, m)
		from[i] = make([]int, m)
		for j := range dp[i] {
			dp[i][j] = neg
		}
	}
	for j := 0; j < m; j++ {
		if eq(p[0], t[j]) {
			dp[0][j] = scoreMatch + bonus[j] - j/4
			from[0][j] = -1
		}
	}
	for i := 1; i < n; i++ {
		// best is the max over k <= j-2 of dp[i-1][k] + penaltyGapChar*k, so
		// the gap cost penaltyGapChar*(j-1-k) can be applied in O(1).
		best, bestJ := neg, -1
		for j := i; j < m; j++ {
			if k := j - 2; k >= 0 && dp[i-1][k] != neg && dp[i-1][k]+penaltyGapChar*k > best {
				best, bestJ = dp[i-1][k]+penaltyGapChar*k, k
			}
			if !eq(p[i], t[j]) {
				continue
			}
			s, f := neg, -1
			if dp[i-1][j-1] != neg {
				s, f = dp[i-1][j-1]+bonusConsecutive, j-1
			}
			if best != neg {
				if g := best - penaltyGapChar*(j-1) - penaltyGapOpen; g > s {
					s, f = g, bestJ
				}
			}
			if s != neg {
				dp[i][j] = s + scoreMatch + bonus[j]
				from[i][j] = f
			}
		}
	}
	bestScore, end := neg, -1
	for j := 0; j < m; j++ {
		if dp[n-1][j] > bestScore {
			bestScore, end = dp[n-1][j], j
		}
	}
	if end < 0 {
		return Match{}, false
	}
	idx := make([]int, n)
	for i, j := n-1, end; i >= 0; i-- {
		idx[i] = j
		j = from[i][j]
	}
	// Prefer shorter targets for equal matches.
	return Match{Score: bestScore - m/8, Indices: idx}, true
}

// Words reports whether every word of query appears in text (case-insensitive).
func Words(query, text string) bool {
	text = strings.ToLower(text)
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

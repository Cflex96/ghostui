// Package ghostty wraps the ghostty CLI: the option schema, themes, fonts,
// actions, validation and live reload. Nothing about Ghostty's options is
// hardcoded here; it is all read from the installed binary.
package ghostty

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CLI runs the ghostty binary.
type CLI struct {
	Bin string
}

// Find locates the ghostty binary: $GHOSTTY_BIN, then the app bundle in
// /Applications or ~/Applications (the macOS CLI usually isn't on PATH),
// then PATH.
func Find() (*CLI, error) {
	var cands []string
	if b := os.Getenv("GHOSTTY_BIN"); b != "" {
		cands = append(cands, b)
	}
	cands = append(cands, "/Applications/Ghostty.app/Contents/MacOS/ghostty")
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, "Applications/Ghostty.app/Contents/MacOS/ghostty"))
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return &CLI{Bin: c}, nil
		}
	}
	if p, err := exec.LookPath("ghostty"); err == nil {
		return &CLI{Bin: p}, nil
	}
	return nil, errors.New("ghostty binary not found (set GHOSTTY_BIN)")
}

// Run executes ghostty with args and returns stdout and stderr. A timeout
// keeps a hung CLI from freezing whatever is waiting on it.
func (c *CLI) Run(ctx context.Context, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Version returns the first line of `ghostty --version`, e.g. "Ghostty 1.3.1".
func (c *CLI) Version(ctx context.Context) string {
	out, _, _ := c.Run(ctx, "--version")
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(line)
}

var reVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

// OlderThan reports whether version ("Ghostty 1.2.3", as Version returns
// it) is older than major.minor. An unreadable version isn't.
func OlderThan(version string, major, minor int) bool {
	m := reVersion.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	ma, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	return ma < major || ma == major && mi < minor
}

// Validate runs `+validate-config --config-file=path` and returns the reported
// problems, one per line. Only that file is loaded, not the default files.
func (c *CLI) Validate(ctx context.Context, path string) ([]string, error) {
	out, errOut, err := c.Run(ctx, "+validate-config", "--config-file="+path)
	var problems []string
	for _, l := range strings.Split(out+"\n"+errOut, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			problems = append(problems, l)
		}
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, err
	}
	return problems, nil
}

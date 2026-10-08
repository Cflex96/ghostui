package ghostty

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ActiveConfigPath returns the file Ghostty's open_config opens: the first
// non-empty file of the App Support and XDG candidates (Ghostty 1.3,
// src/config/edit.zig). With none, it is the classic XDG path.
func ActiveConfigPath() string {
	home, _ := os.UserHomeDir()
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	var cands []string
	if runtime.GOOS == "darwin" {
		app := filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty")
		cands = append(cands, filepath.Join(app, "config.ghostty"), filepath.Join(app, "config"))
	}
	cands = append(cands, filepath.Join(xdg, "ghostty", "config.ghostty"), filepath.Join(xdg, "ghostty", "config"))
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return c
		}
	}
	return filepath.Join(xdg, "ghostty", "config")
}

// SameFile reports whether a and b are the same file (following symlinks).
func SameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// CanReload reports whether this platform has a reload path.
func CanReload() bool { return runtime.GOOS == "darwin" }

// ErrNotRunning means Ghostty isn't running or has no window to reload in.
var ErrNotRunning = errors.New("ghostty isn't running")

// reloadScript performs reload_config on the front window. The `is running`
// guard keeps `tell` from launching Ghostty.
const reloadScript = `if application id "com.mitchellh.ghostty" is running then
	tell application id "com.mitchellh.ghostty"
		if (count of windows) is 0 then return "no window"
		perform action "reload_config" on focused terminal of selected tab of front window
	end tell
else
	return "not running"
end if`

// Reload asks the running Ghostty to reload its config through AppleScript.
// It never launches Ghostty.
func Reload(ctx context.Context) error {
	if !CanReload() {
		return errors.New("live reload needs macOS")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", reloadScript).CombinedOutput()
	res := strings.TrimSpace(string(out))
	switch {
	case err != nil && res == "":
		return err
	case err != nil:
		return errors.New(res)
	case res == "not running", res == "no window":
		return ErrNotRunning
	case res != "true":
		return errors.New("reload_config returned " + res)
	}
	return nil
}

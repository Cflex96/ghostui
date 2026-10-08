// Command ghostui is a TUI for Ghostty's config.
//
// Usage: ghostui [flags] [config-path]
//
// Without a path it edits the file Ghostty's open_config opens. Changes stay
// unsaved until ⌃s; every change is validated with `ghostty +validate-config`
// first. When the edited file is the active config, unsaved changes are
// previewed live: written to the file and reloaded, then taken back out
// again unless they're saved.
//
// GHOSTUI_FORCE_PREVIEW=1 previews into any file, without reloading
// Ghostty. It exists for testing against scratch copies.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/Cflex96/ghostui/internal/ghostty"
	"github.com/Cflex96/ghostui/internal/ui"
)

// version is set by release builds (-ldflags "-X main.version=v1.2.3");
// otherwise it comes from the module version go install records.
var version = ""

func main() {
	noReload := flag.Bool("no-reload", false, "don't preview unsaved changes in Ghostty or reload after saving")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ghostui [-no-reload] [config-path]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("ghostui", buildVersion())
		return
	}

	cli, err := ghostty.Find()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghostui:", err)
		os.Exit(1)
	}
	active := ghostty.ActiveConfigPath()
	path := active
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	// Previewing only makes sense for the file Ghostty actually loads.
	live := !*noReload && ghostty.CanReload() && (path == active || ghostty.SameFile(path, active))
	opt := ui.Options{Path: path, CLI: cli, Preview: live, Reload: live}
	if os.Getenv("GHOSTUI_FORCE_PREVIEW") == "1" {
		opt.Preview = true
	}

	app := ui.New(opt)
	p := tea.NewProgram(app)

	// Bubble Tea handles SIGINT and SIGTERM (Run returns); SIGHUP, which the
	// terminal sends when its window closes, would kill the process with a
	// preview still on disk. Take it back out first.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		<-hup
		app.Shutdown()
		p.Kill()
	}()

	_, err = p.Run()
	if serr := app.Shutdown(); serr != nil {
		fmt.Fprintln(os.Stderr, "ghostui: putting the saved config back:", serr)
	}
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		fmt.Fprintln(os.Stderr, "ghostui:", err)
		os.Exit(1)
	}
}

// buildVersion is the module version (v0.1.0 from go install …@v0.1.0), or
// for a build from a checkout, the commit it was built from.
func buildVersion() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value[:min(12, len(s.Value))]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	switch {
	case rev == "":
		return "devel"
	case dirty:
		return "devel " + rev + "-dirty"
	}
	return "devel " + rev
}

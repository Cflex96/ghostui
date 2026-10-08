# ghostui — notes for working on the code

`ghostui` is a TUI for Ghostty's config, written in Go with Bubble Tea v2. `README.md` covers what it does for users; this file covers how it works and how to work on it safely. Machine-specific rules, if any, are in `CLAUDE.local.md` (git-ignored).

## Rules

- **Never point the TUI at the live config yourself.** It writes whatever file it's given, and with live preview it writes unsaved changes too. Test on a scratch copy (`ghostui /path/to/copy`). Running it on the live config is the human's step.
- **Never quit, kill or restart Ghostty**, and never `close window` through AppleScript. The human is working in it.
- **Permission dialogs, key presses and menu clicks are human-only.** Ask for them. Never automate them through System Events or Accessibility, and never edit the TCC database.

## Commands

```bash
go vet ./... && go test ./...                         # uses the installed Ghostty, never reloads it
go build -o ghostui ./cmd/ghostui                     # git-ignored
GHOSTUI_FORCE_PREVIEW=1 ./ghostui /path/to/copy       # preview into a scratch copy, no reload
./ghostui -version                                    # module version, or the commit for a local build
```

CI (`.github/workflows/ci.yml`) runs gofmt, vet and `go test -race` on macOS with Ghostty from Homebrew, and on Linux without it (the Ghostty-backed tests skip there).

To drive the TUI by hand, use a separate tmux server (`tmux -L ghostui-test`) and a scratch copy of a config, so nothing touches the human's sessions.

- On macOS the `ghostty` CLI usually isn't on `PATH`. Use `/Applications/Ghostty.app/Contents/MacOS/ghostty` (`+show-config`, `+validate-config`, `+list-actions`). `ghostty.Find()` locates it for the TUI.
- `GHOSTUI_FORCE_PREVIEW=1` turns preview on for any file without reloading Ghostty. It's a test hook for scratch copies. Never combine it with the live config.
- `GHOSTUI_STATE_DIR` moves the state directory. Tests set it to a temp dir.

## How it works

1. **Which file.** Without a path, `ghostty.ActiveConfigPath()` (`internal/ghostty/reload.go`) picks the file Ghostty's `open_config` opens (`src/config/edit.zig`, `configPath()` in Ghostty 1.3.1): the first **non-empty** file of
   1. `~/Library/Application Support/com.mitchellh.ghostty/config.ghostty` (macOS)
   2. `…/com.mitchellh.ghostty/config` (macOS)
   3. `$XDG_CONFIG_HOME/ghostty/config.ghostty`
   4. `$XDG_CONFIG_HOME/ghostty/config`
2. **Schema.** `internal/ghostty` reads options, docs and defaults from `+show-config --default --docs`, and infers types by probing `+validate-config`. Nothing about Ghostty's options is hardcoded.
3. **Validate, then write.** `config.Draft` does every write. It writes the new bytes to a temp file next to the real one (so relative `config-file` includes resolve), validates it with `ghostty +validate-config`, then renames it into place. Writes replace a symlink's target, never the symlink, and keep the file's mode.
4. **Preview and reload.** Only when the file is the active config (same path or same file): unsaved changes are written to the file and `ghostty.Reload` runs `perform action "reload_config"` through `/usr/bin/osascript` on the front window, guarded by `is running` so it never launches Ghostty. Discarding or quitting without saving puts the saved bytes back. When Ghostty isn't running or has no window, `Reload` returns `ErrNotRunning`, which the UI treats as "nothing to reload", not an error. `ui.New` turns preview off when the Ghostty is older than 1.3 (no AppleScript) or the config sets `macos-applescript = false`.
5. **Outside edits.** The UI polls the file's mtime and size. `Draft.Rebase` takes an edit in; with a preview on disk, the unsaved changes are taken back out of the edited file (`unstage`). The UI then re-applies its unsaved changes on top with `config.Patch`: where the edit changed the same value, the edit wins and the unsaved change goes. `Draft.Show` returns `ErrChanged` rather than restoring over an edit it hasn't reported yet.
6. **Includes.** `Store.Includes` follows `config-file` entries (relative to the config's real directory, `?` optional, `~/` expanded, cycles skipped) and returns what they set. Ghostty loads includes after the file, so the UI marks those keys `include` and says which value wins. Includes are read, never edited.
7. **Signals.** Bubble Tea handles SIGINT and SIGTERM. `cmd/ghostui/main.go` catches SIGHUP (sent when the window closes) and takes the preview back out before exiting.
8. **State.** `internal/state` keeps `history.json` (saved changes, for "recent" and undo) and the recovery journal `unsaved-<sha256(path)[:16]>.json` in `$GHOSTUI_STATE_DIR`, else `$XDG_STATE_HOME/ghostui`, else `~/.local/state/ghostui`. The journal is written before every preview write (and after each validation when preview is off). On the next start, `Draft.Recover` uses it to put the saved config back if a preview was left on disk, then offers the changes again as unsaved.

## Files

| Path | Role |
|---|---|
| `cmd/ghostui` | Entry point: flags (`-no-reload`, `-version`), picks the file, decides whether to preview, signal handling. |
| `internal/ghostty` | Wraps the `ghostty` CLI: schema, themes, fonts, actions, validation, reload. |
| `internal/config` | Line-preserving round-trip edits (`file.go`). `Draft` (`draft.go`) does every write: validate-before-rename, previews of unsaved changes, save, restore, the recovery journal. |
| `internal/state` | Undo history and journal paths. |
| `internal/fuzzy` | Fuzzy matching for the pickers. |
| `internal/ui` | The screens. `save.go` is the unsaved-changes flow, `changes.go` the review overlay and quit dialog. |
| `design/` | The design the TUI was built from: `DESIGN.md`, the `*.dc.html` mockups, and the plan for the unsaved-changes / live-preview rework. Historical; the code is the reference now. |
| `.github/` | CI workflow and the bug report template. |

## Status

- Verified only on Ghostty 1.3.1 and macOS 26.5 (Apple Silicon), with unit tests and by driving it in `tmux -L ghostui-test` on scratch copies. Not yet verified against a live config.
- Linux: builds and should run with preview off (`CanReload()` is macOS-only). Untested.
- The `no window` branch of the reload AppleScript compiles (`osacompile`) but hasn't been run.
- Super-key capture in the keybind recorder is untested directly in a Ghostty window (outside tmux), where the kitty keyboard protocol is on.

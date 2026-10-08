# ghostui

A terminal UI for [Ghostty](https://ghostty.org)'s config file. Search every option with its docs, change values with sliders, toggles and pickers, try themes and keybinds, and watch Ghostty update live before you save.

Nothing about Ghostty's options is hardcoded. The option list, docs, defaults, themes, fonts and actions are all read from the Ghostty you have installed.

ghostui is an independent project, not affiliated with Ghostty.

## Status

Early. It has been tested with **Ghostty 1.3.1 on macOS 26 (Apple Silicon)**. It writes your real config, so keep a backup (or keep the config in git) until you trust it. Bug reports are welcome; please include `ghostui -version` and `ghostty --version`.

## Features

- **Search everything**: `/` searches every option by name and docs.
- **Edit by type**: sliders for numbers, toggles for bools and flags, pickers for enums and fonts, a swatch next to colors, free text for the rest.
- **Theme studio**: browse themes with a preview, pin some to compare, and set separate dark and light themes.
- **Keybinds**: record a binding by pressing the keys, pick an action, unbind defaults.
- **Live preview** (macOS): each change is shown in Ghostty right away, but nothing is saved until you press `⌃s`. If you quit without saving, the saved config is put back.
- **Safe writes**: every change is checked with `ghostty +validate-config` first, and rejected changes are never written. Writes are atomic, keep the file's permissions and follow symlinks, so a config that lives in a dotfiles repo works. Comments and formatting are kept, and only the lines you change are touched.
- **Undo, review and recovery**: `u`/`U` undo and redo. `s` shows unsaved changes as a diff and lets you revert single ones. If ghostui dies while a preview is on disk, the next start puts the saved config back and offers the changes again.

## Requirements

- Ghostty 1.3 or newer. ghostui looks in `/Applications/Ghostty.app`, `~/Applications/Ghostty.app` and your `PATH`. Set `GHOSTTY_BIN` to use another binary. With an older Ghostty it says so at start and may not work.
- Go 1.26.2 or newer, to build it. With Go 1.21 or newer, `go install` fetches that toolchain itself.
- macOS for live preview. Reloading uses Ghostty's AppleScript support (`perform action "reload_config"`, added in Ghostty 1.3), so `macos-applescript` has to stay on, which is the default. On Linux it should run with live preview off, but that hasn't been tested.

## Install

```bash
go install github.com/Cflex96/ghostui/cmd/ghostui@latest
```

This puts `ghostui` in `$(go env GOPATH)/bin` (usually `~/go/bin`), which needs to be on your `PATH`. `ghostui -version` prints the version you have.

## Usage

```bash
ghostui                    # edit the config Ghostty uses, with live preview
ghostui -no-reload         # same file, no preview and no reload
ghostui path/to/config     # any other file: no preview, no reload
```

Without a path, ghostui edits the same file as Ghostty's *Settings…* menu: the first non-empty file of

1. `~/Library/Application Support/com.mitchellh.ghostty/config.ghostty` (macOS only)
2. `~/Library/Application Support/com.mitchellh.ghostty/config` (macOS only)
3. `$XDG_CONFIG_HOME/ghostty/config.ghostty` (default `~/.config/ghostty/config.ghostty`)
4. `$XDG_CONFIG_HOME/ghostty/config`

Live preview is only on for that file. If you run ghostui from a terminal other than Ghostty, macOS may ask once for permission to control Ghostty. If Ghostty isn't running, there's nothing to preview in: changes are still checked and saved, just not shown.

If you edit the config in another program while ghostui is open, ghostui takes the edit in. Unsaved changes in ghostui stay unsaved, except where your edit changed the same option: then your edit wins.

Options set in a file the config includes (`config-file`) are marked `include`. Ghostty loads included files after the config, so their values win over the ones ghostui edits; the docs panel says which file sets what.

## Keys

Press `?` in ghostui for the full list.

| Key | Does |
|---|---|
| `/` | search every option |
| `tab` `⇧tab`, `o a f w k t c m` | switch tabs |
| `↑↓`, `←→` (`⇧←→` for fine steps) | move, adjust a value |
| `space` | toggle a bool or flag |
| `⏎` | edit, or open themes, keys and fonts |
| `⌫` | reset to the default |
| `⌃s` | save every unsaved change |
| `s` | review unsaved changes, revert single ones |
| `u` `U` | undo, redo |
| `R` | reload Ghostty's config now |
| `q` `⌃c` | quit (asks about unsaved changes) |

## Files it keeps

Apart from the config file (and short-lived temp files next to it while it validates and writes), ghostui writes nothing into Ghostty's config folders. Its own state lives in `$XDG_STATE_HOME/ghostui` (default `~/.local/state/ghostui`; override with `GHOSTUI_STATE_DIR`):

- `history.json`: your last 200 saved changes, for the *recent* list and undo.
- `unsaved-<hash>.json`: your unsaved changes, so they survive a crash. It holds a copy of your saved config (mode 0600) and is removed when you save or discard.

## Known limits

- After a `kill -9` or a power loss with a preview on disk, the config keeps the preview until ghostui starts again. If Ghostty restarts in between, it loads the preview.
- SIGTERM and SIGHUP (closing the window) put the saved config back and keep the unsaved changes for the next start. Edits made in the last ~280 ms before that are lost.
- ghostui edits one file. Included files (`config-file`) are read to mark what they override, but not edited.
- It needs to write next to the config: every check and save goes through a temp file in the same folder. A read-only config, such as one managed by Nix or home-manager, can be browsed but not changed.
- The terminal needs to be at least 30×10.
- Capturing the Super key in the keybind recorder hasn't been tested directly in a Ghostty window (only inside tmux).

## Development

```bash
go vet ./... && go test ./...
```

The tests use the installed Ghostty to read the schema and validate configs, and skip those parts when Ghostty isn't installed. They never reload Ghostty. CI runs them on macOS with Ghostty installed from Homebrew, and on Linux without it. To try changes by hand, run ghostui on a copy of your config: `ghostui /tmp/config-copy`.

`design/` holds the design ghostui was built from. `CLAUDE.md` explains how the code fits together.

## License

MIT, see [LICENSE](LICENSE).

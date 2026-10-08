# Unsaved changes, live preview and the save dialog

Date: 2026-10-02. Scope: `internal/config`, `internal/state`, `internal/ui`, `cmd/ghostty-settings` (now `cmd/ghostui`).

## Goal

Today every edit is written to the config 280 ms after the last key press, and Ghostty reloads it. From now on:

1. **Nothing is saved until the human saves.** `⌃s` saves every unsaved change; the review overlay (`s`) shows them first.
2. **Unsaved changes are still previewed live in Ghostty.** The working copy is written to the config temporarily and Ghostty reloads it. Discarding, or quitting without saving, writes the saved version back. (Decided with the human, 2026-10-02: "temp write, restore".)
3. **The changelog is the list of unsaved changes.** The human can revert single entries (`x`). Whatever is left at the end is what gets saved.
4. **Quitting with unsaved changes asks first:** save & quit, discard & quit, or keep editing.

## Terms

- **saved**: the config as last loaded or saved (`App.file`).
- **staged**: unsaved values that passed validation, one full value list per key (`App.staged`).
- **pending / inflight**: edits waiting for the debounce or being validated. Same roles as today.
- **working copy**: saved + staged + inflight + pending, in first-touch order of keys.
- **preview mode**: on when the edited file is Ghostty's active config and live reload works (today's `Reload`). Then the working copy is on disk while unsaved changes exist. Off for scratch copies: the disk never changes before a save.

## Design

### `config.Draft`: the only thing that writes the file

Every write goes through one mutex, so a hangup in the middle of a preview write can't race a restore.

| Method | Does |
|---|---|
| `Load()` | Reads the file and makes it the saved version. |
| `Check(data, staged)` | Validates the working copy without writing it (preview off). Updates the journal. |
| `Show(data, staged)` | Validates, writes the journal, then atomically writes the working copy (preview on). `data == saved` writes the saved version back and drops the journal. `ErrChanged` if the disk isn't what Draft last wrote or loaded. |
| `Save(data)` | Validates, writes, and makes it the saved version. Drops the journal. |
| `Discard()` | Puts the saved version back if a preview is on disk. Drops the journal. If the file was edited behind the preview, it takes only the unsaved changes back out (`File.Replace` patch), so that edit survives. |
| `Rebase()` | Handles an outside edit noticed by polling. With a preview on disk, the new saved version is the disk minus the unsaved changes. |
| `Close()` | Shutdown (SIGTERM, SIGHUP, crash path in `main`): restores like `Discard` but **keeps** the journal, then refuses further writes. |
| `Recover()` | Startup: reads the journal. If the disk still holds one of our previews, it writes the saved version back. If the disk holds the saved version, nothing is written. Either way it returns the unsaved changes, which come back as unsaved. If the file changed since, it drops them and says so. |

Validation compares against the saved version's problems (cached per saved version), as `Store.Apply` does today. So an already-broken file can still get unrelated edits.

**Journal**: `$STATE/unsaved-<sha256(path)[:16]>.json`, next to `history.json`. It holds the path, the saved bytes, whether the file existed, the hashes of the previews written (the last two, so a crash between the journal write and the file write is covered), and the staged values. It is written before each preview write, and after each validation when preview is off.

### App

- `vals(key)`: pending → inflight → staged → saved.
- `set` / `commit` / editor / theme / keybind paths are unchanged on the outside. `commit` computes the new list with `File.Replace(from, to)` on the working copy, and stages that.
- `flush` validates (and, in preview mode, shows) the working copy. On success, inflight values become staged and an undo step is recorded. On rejection they're dropped with `✗ not applied: …`, as today.
- **Undo** is in memory. The stack is seeded from `history.json` for this path, so `u` still reaches changes saved in earlier sessions. Undoing stages the old value (unsaved), it doesn't write it. On save, this path's history becomes the current stack. Discard resets the stack to the last save point.
- `save` (`⌃s`, `⏎` in the overlay) waits for pending validation, then `Draft.Save`, then records history.
- `discard` (`d` in the overlay) drops staged, pending and inflight, then `Draft.Discard` and a reload.
- **Quit** (`q`, `⌃c`): no unsaved changes means it quits (after `Discard` cleans up any leftover preview). With unsaved changes it opens the overlay in quit mode. `⌃c` inside the dialog means discard & quit.
- **Poll**: an outside edit while staged → `Draft.Rebase`. Staged values that now equal the saved ones are dropped, and the rest are shown again.
- **Startup**: `Recover`. Recovered changes go back to pending, `Init` flushes them (validate + preview), and the flash says `recovered N unsaved changes`.

### UI

- Header: `● live preview` / `○ preview off`; `● N unsaved` in place of `● N changed` while there are unsaved changes. The window title gets a `● ` prefix while dirty.
- Home left column: **UNSAVED** (newest first, saved → working) while dirty, with `s review · ⌃s save`. Otherwise **RECENT**, as today.
- **Changes overlay** (`s`, and on quit): one block per key, with the summary line and the exact config lines as a `-`/`+` diff (line numbers from the saved file). `↑↓` move, `x` reverts the entry, `⏎` saves, `d` discards all, `esc` closes. In quit mode: `⏎` save & quit, `d` discard & quit, `esc` keep editing.
- Footer hint `⌃s save` while dirty. Status after an edit: `previewing · ⌃s saves`, or `not saved yet · ⌃s saves` when preview is off. `checking…` while validating.
- Help and labels: "commit" → "done", text-field "save" → "set", and quit "asks about unsaved changes".

### `main`

- `Options{Preview, Reload}`: both on for the active config. `GHOSTUI_FORCE_PREVIEW=1` turns preview on without reload, as a test hook for scratch copies.
- SIGHUP (window closed, or Ghostty quit): `app.Shutdown()` (= `Draft.Close`), then `p.Kill()`.
- After `p.Run` returns for any reason (SIGTERM included): `app.Shutdown()`.
- `ui.Filter` goes away: SIGTERM no longer flushes edits to disk, because that would persist unsaved changes.

## Edge cases

| Case | Behavior |
|---|---|
| Invalid value | Rejected before anything reaches the disk, as today. |
| Edit behind the preview (nvim) | Poll rebases. The outside edit becomes part of the saved version, and unsaved changes stay unsaved. |
| Window closed / Ghostty quits (HUP) | Saved version written back. Unsaved changes are offered again at the next start. |
| `kill -9`, power loss | The preview stays on disk until the next start, which repairs it. **Known limit:** a Ghostty restart before then loads the preview. |
| validate.sh SIGTERM | No unsaved changes in those runs, so nothing changes. Otherwise the same as HUP. |
| Two TUIs on one file | Not supported (shared journal). Same as today's shared history. |

## Rollout

1. Fill test gaps against today's behavior (done: `state` had no tests; `config`, `ghostty` and the UI flows had gaps).
2. `state`: `Add` (the pure coalescing step), `For`, `Replace`, `Journal`. Tests.
3. `config.Draft` + journal + recovery. Tests with a fake validator, including the outside-edit, crash-between-writes and created-file cases.
4. App: staging, preview, save/discard, undo stack, rebase, recovery, shutdown. Rewrite the flow tests that pinned auto-save to the new semantics, and add tests for the preview, the dialog, revert-one-entry, shutdown + recovery and rebase.
5. UI: overlay, header, left column, hints, help. Render test at every size.
6. `main`: options, HUP, shutdown.
7. Verify: `go vet`, `go test -race ./...`, `bash src/test-tui.sh`. Then build and drive the binary in `tmux -L ghostui-test` on a scratch copy with `GHOSTUI_FORCE_PREVIEW=1`: edit → file changes → `q` → dialog → discard → bytes back. Repeat with save, with `kill -HUP` and with `kill -TERM`, then restart → recovered.
8. Update `GHOSTTY-SETTINGS-POC.md` (files, journal, known limits).

Not done here: running against the live config or in the Cmd+Comma window. That stays a human step, as before.

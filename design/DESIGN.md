# ghostty/settings — design spec

> Historical: this is the spec ghostui was first built from, kept for
> reference. The code is the reference for how ghostui works now.

Mockups: the `*.dc.html` files in this folder are the HTML source of the
design canvas. They need the canvas runtime (`support.js`), which isn't in
this repository, so read them as source. They were drawn in HTML, so
translate them to the terminal cell grid using the rules at the bottom.

Stack: Go + Bubble Tea + Lip Gloss (+ Bubbles for textinput/list/viewport).

## Tokens (truecolor, Lip Gloss)

| token   | hex       | use                                       |
|---------|-----------|-------------------------------------------|
| bg      | `#0C0C0E` | app background                            |
| surface | `#111114` | docs panel, cards                         |
| raised  | `#1D1D22` | selected row background                   |
| border  | `#26262C` | rounded borders, rules                    |
| text    | `#ECEAE4` | primary text, active keys                 |
| muted   | `#9A978F` | labels, unselected keys, hints            |
| faint   | `#5E5C57` | types, defaults, section headers          |
| accent  | `#FF7A3D` | cursor `▌`, active tab bg, fuzzy matches  |
| ok      | `#7DD3B0` | valid / saved / reloaded / diff `+`       |

One accent only. Everything secondary is dimmer, never another color.

In the TUI these follow the terminal's color scheme (2026-10-02). The
background is the terminal's own and is never painted; text is its
foreground; surface, raised, border, muted and faint are blends of the two,
at the ratios the values above have over `#0C0C0E` / `#ECEAE4`. Accent, ok
and errors are ANSI blue, green and red. The table is the fallback until
the terminal reports its colors (`internal/ui/styles.go`).

## Screens

1. **Home (Main.dc.html)** — header `ghostty / settings` + status (`● live reload`,
   `✓ config valid`, `● N changed`). Pill tabs with underlined hotkey letter.
   Left: half-block pixel ghost in accent (12×12 pixels, drawn square: one
   column and half a row each), tagline, "recent" change log.
   Right: ONLY overridden options. Row = key · type-specific value widget ·
   type label. Selected row: `raised` bg + accent `▌`. Docs panel below
   follows the cursor (key, type, default, description, related options).
2. **Palette (Palette.dc.html)** — `/` opens a centered overlay over the dimmed
   home screen. Fuzzy match on keys AND doc text; matched runes in bold accent.
   Selected option expands inline: slider, live preview, docs. Esc closes.
3. **Theme Studio (Themes.dc.html)** — left: filter + theme list, each with an
   8-swatch strip (bg, red, green, yellow, blue, magenta, cyan, fg). Right:
   sample shell/git/test/code output rendered in the highlighted theme's
   colors + labeled palette legend. Dark / light / both toggle writes
   `theme = light:X,dark:Y`.
4. **Keybind Recorder (Keys.dc.html)** — big keycaps for the captured combo,
   fuzzy action search, conflict detection against the user's config shown
   as a `-`/`+` diff of the exact line, grouped list of custom keybinds.

Footer on every screen: keycap-style hints (`[/] search  [⏎] edit  …`).

## Value widgets

- float/int in a range → `━━━━━━━●━━` track or `▰▰▰▰▱` bar + number
- enum → inline segmented picker, active option on `#2A2A30` bg
- bool → `on` / `off` toggle
- color / theme → `██` swatches in truecolor
- font → name + fallbacks, opens `ghostty +list-fonts` sub-search

## Data sources (never hardcode)

- options, types, defaults, docs: `ghostty +show-config --default --docs`
- themes: `ghostty +list-themes`; fonts: `ghostty +list-fonts`
- keybinds/actions: `ghostty +list-keybinds`, `ghostty +list-actions`
- validate after every write: `ghostty +validate-config`
- edit the config file round-trip: keep comments, order, repeated keys

## Translating HTML → terminal

- One font size. Large type = block characters (like the ghost).
- No rounded backgrounds: selection = full-width bg bar; rounded corners only
  via `lipgloss.RoundedBorder()`.
- No shadows/glow: the overlay is separated by a border + dimmed backdrop
  (re-render the background in `faint`).
- Respect cell widths; measure with `lipgloss.Width`, never `len`.

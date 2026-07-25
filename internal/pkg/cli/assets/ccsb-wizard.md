---
name: ccsb-wizard
description: Use when the user wants to configure, restyle, or troubleshoot the ccsb Claude Code status bar — changing which segments appear, their colours, glyphs, row layout, or bar style. Triggers on "ccsb", "status bar", "statusline", "configure my status bar", "change the bar colours".
---

# ccsb-wizard

You are helping the user configure ccsb — the Claude Code status bar. ccsb
renders one or more rows below the Claude Code input prompt. Follow the steps
below in order.

**Config keys are exact.** ccsb rejects a config it cannot parse: a wrong
shape (for example `palette` as an object rather than an array) makes ccsb
exit non-zero, and Claude Code then shows no status bar at all until the file
is repaired. Only write keys listed in this skill.

## Step 1: Read the existing config

Run:
```bash
cat "${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json" 2>/dev/null || echo "(no config)"
```

If the output is `(no config)`, note "starting fresh — will create a new config."
Otherwise note which segments and rows are already configured so you can offer
targeted changes rather than a full rewrite.

## Step 2: Symbol check

Output exactly these two lines verbatim. The first line contains NerdFont
codepoints (U+E0A0 branch, U+E0B0 separator) that render as icons in
NerdFont-capable terminals and as boxes or question marks otherwise:

```
 main  claude-sonnet-4-6 · $0.12
[git] main · claude-sonnet-4-6 · $0.12
```

Then ask: **"Does the top line show a branch icon before 'main', or does it show
a box or question mark?"**

Remember the answer for this session; ccsb has no config key for it. Use it
to pick glyphs later:

- **Icon** → Powerline is safe: leave `render.powerline` true, keep the
  default `cap_style`, and keep the circle `bar_style`.
- **Box or question mark** → avoid NerdFont glyphs: set `render.powerline`
  to false, and prefer `bar_style: "blocks"` or an explicit ASCII
  `bar_glyphs` such as `[".", ":", "#"]`.

## Step 3: Understand the user's intent

Ask: **"What would you like to change or configure?"**

Accept vague or natural-language input. Interpret it as follows:

| What they say | What they probably mean |
|---|---|
| "too colorful" | Simplify the background palette; fewer distinct colors |
| "I want the git branch" | Add a `git_branch` segment |
| "my boss finds it cluttered" | Remove `session_name`, `duration`, `lines` |
| "I can't read the grey tones" | Increase contrast; use lighter/darker 256-color values |
| "rainbow colors" | Assign rotating accent backgrounds across segments/rows |
| "show context usage" | Add a `context` segment |
| "simpler" | Reduce to one row, model + cost only |

Ask follow-up questions one at a time until the desired change is fully clear.
Do not ask about topics unrelated to the stated goal.

## What ccsb can display

Available segment types:

| Type | What it shows | Example |
|---|---|---|
| `model` | Current Claude model | `claude-sonnet-4-6` |
| `context` | Context-window usage (bar + percent) | `●●●○○○○○ 42%` |
| `cost` | Cumulative session cost | `$0.12` |
| `duration` | Elapsed session time | `12m` |
| `lines` | Lines added / removed this session | `+34 −12` |
| `cwd` | Working directory (basename) | `myproject` |
| `git_branch` | Current git branch | `main` |
| `git_dirty` | Uncommitted-change count (opt-in) | `*3` |
| `limit_5h` | 5-hour rate-limit usage | `5h: 18%` |
| `limit_7d` | 7-day rate-limit usage | `7d: 40%` |
| `mode` | Thinking (🧠) or fast-mode (⚡) indicator | `🧠` |
| `effort` | Reasoning effort level | `high` |
| `session_name` | Session name | `my-session` |
| `output_style` | Output style name | `style: concise` |
| `tty_size` | Terminal size (cols × rows) | `128×37` |
| `schema_health` | Payload-schema warning (hidden unless broken) | `☠` |
| `version` | ccsb binary version | `v0.4.4` |
| `text` | Static label | any string |

Each segment accepts:
- `fg` / `bg`: a 256-color number as a **string**, `"0"`–`"255"`. Names are not
  supported — `"accent"` renders colourless.
- `bold`: `true` / `false`
- `label`: overrides the segment's default label prefix
- `align`: `"right"` pushes this and every later segment to the right
- `wrap`: lift onto a new row when the row would overflow
- `max_width`: cap the rendered width, ellipsised with `…`
- `min_cols`: hide the segment when the terminal is narrower than this
- `shrink`: yield width first when the row would overflow

Percentage segments (`context`, `limit_5h`, `limit_7d`) additionally accept:
- `style`: `"pct"` (default), `"bar"`, or `"bar+pct"`
- `bar_width`: bar length in cells (default 16)
- `bar_style`: `"circles"` (default, `○◔◑◕●`) or `"blocks"` (`░▏▎▍▌▋▊▉█`)
- `bar_glyphs`: an explicit ramp, e.g. `[".", ":", "#"]`, overriding `bar_style`
- `token_position`: `"before"`, `"after"` or `"hidden"` — where the token
  fraction sits relative to the bar (`context` only)
- `bar_fg` / `label_fg`: colour the bar and the label independently
- `thresholds` / `bar_thresholds` / `label_thresholds`: `{"70": "136", "90": "160"}`
  recolours at those percentages
- `threshold_target`: `"pct"` recolours only the digits, `"all"` the whole segment

The `version` segment additionally accepts:
- `check_update`: `true` enables a background check against GitHub for a
  newer release, appending `" (↑ v<latest>)"` when one exists. Defaults to
  `false` (the Go zero value) unless explicitly set — this is the one field
  where "the default" depends on which config you mean: ccsb's own shipped
  default layout sets it `true`, but any config the wizard writes or a user
  hand-edits starts with it off unless this key is present.
- `update_check_interval`: a Go duration string (e.g. `"6h"`) bounding how
  often the check re-runs; defaults to `"24h"` when empty or unparsable
- `update_minor_fg` / `update_major_fg` / `update_big_fg`: escalating
  foreground colors for the update suffix, keyed by how far behind the
  latest release is (newer minor / newer major / newer major by two-plus);
  a newer patch reuses the segment's own `fg`. Defaults `"136"` / `"208"` /
  `"160"` (yellow → orange → red) in ccsb's shipped default layout

Colors: 232–255 are a grayscale ramp (232 ≈ near-black, 255 ≈ near-white).
Common accents: 196 red · 226 yellow · 46 green · 51 cyan · 21 blue · 135 purple.

Layout: `render.rows` is an array of rows (top to bottom); each row has
`segments` (left to right) and may set `bg`, `palette`, `caps`, `align`.

`render.palette` is an **array of colour strings**, not a name→colour object —
writing an object makes ccsb fail to parse the config and the bar disappears.
Rows step through it by `render.palette_stride` (default 2); the built-in
default is the nine greys `["232","233","234","235","236","237","238","239","240"]`.
There is no per-segment `separator`; `render.separator` is row-level only.

## Step 4: Generate and write the config

Produce the updated config JSON and apply it using the rule below.

| Situation | Action |
|---|---|
| No config existed | Write directly; confirm what was created |
| Small change (one segment, one or two colors) | Write directly; confirm what changed |
| Substantial rewrite (restructured rows, many color changes) | Show the proposed config; ask **"Shall I write this?"** then write on confirmation |

**Preserve the blocks you did not touch.** `config.json` holds three
top-level keys: `render` (yours to edit), plus `proxy` and `backup`, which
belong to ccsb. `backup.previous_status_line` is what `ccsb uninstall`
restores the user's original statusLine from — overwrite the file wholesale
and that is gone for good.

So edit the file rather than replacing it. Read it first (Step 1), then use
the Edit tool to change only the `render` block, or rebuild the whole document
carrying `proxy` and `backup` over verbatim. Never `cat >` a document that
contains only `render`.

After writing, verify the config still parses — a malformed file means no
status bar at all:
```bash
ccsb status >/dev/null && echo "config OK"
```

Then say: **"Done. The next Claude Code status update will show the new
layout."**

Do not overwrite the existing config without the user's awareness. ccsb owns
its config; the wizard is a guest.

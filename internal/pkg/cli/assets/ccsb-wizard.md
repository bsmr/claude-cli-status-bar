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

- **Icon** → Powerline is safe. You must still write it explicitly:
  `"powerline": true` together with `"powerline_style": "solid"` and
  `"cap_style": "round"`, and keep the circle `bar_style`.
- **Box or question mark** → avoid NerdFont glyphs: set `render.powerline`
  to false, and prefer `bar_style: "blocks"` or an explicit ASCII
  `bar_glyphs` such as `[".", ":", "#"]`.

**There is no "leave it as it is" for Powerline.** `powerline` is a plain
boolean whose zero value is false, and ccsb fills in its Powerline defaults
*only* when the config has no `rows` at all. The moment you write a `rows`
array — which is the only thing this skill ever does — omitting the key turns
Powerline OFF: no backgrounds, no chevrons, and `caps` silently ignored.
Omitting `powerline_style` likewise downgrades the shipped solid wedge
(U+E0B0) to a thin line (U+E0B1). Carry all three keys in every config you
write.

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
| `model` | Current Claude model | `Sonnet 4.6` |
| `context` | Context usage: bar, percent and token fraction | `●●●●●●◕○○○○○○○○○ 42% 84k/200k` |
| `cost` | Cumulative session cost | `$0.12` |
| `duration` | Elapsed session time | `12m` |
| `lines` | Lines added / removed this session | `+34 −12` |
| `cwd` | Working directory (basename) | `myproject` |
| `git_branch` | Current git branch | `main` |
| `git_dirty` | Uncommitted-change count (opt-in) | `*3` |
| `limit_5h` | 5-hour rate-limit usage, with reset countdown | `5h: 18% · 2h30m` |
| `limit_7d` | 7-day rate-limit usage, with reset countdown | `7d: 40% · 4d15h` |
| `mode` | Thinking (🧠) or fast-mode (⚡) indicator | `🧠` |
| `effort` | Reasoning effort level | `effort: high` |
| `session_name` | Session name | `my-session` |
| `output_style` | Output style name | `style: concise` |
| `tty_size` | Terminal size (cols × rows) | `128×37` |
| `schema_health` | Payload-schema warning (hidden unless broken) | `☠` |
| `version` | ccsb binary version | `v0.4.4` |
| `text` | Static label | any string |

Each segment accepts:
- `type`: **required** — one of the names above. A segment without it renders
  nothing.
- `fg` / `bg`: a 256-color number as a **string**, `"0"`–`"255"`. Names are not
  supported — `"accent"` renders colourless.
- `bold`: `true` / `false`
- `label`: overrides the segment's default label prefix. Honoured by
  `text` (where the label IS the body), `effort`, `output_style`, `limit_5h`,
  `limit_7d`, `git_branch`, `git_dirty` and `tty_size` **only** — the other ten
  types never read it, so setting it there does nothing.
- `align`: `"right"` pushes this and every later segment to the right
- `wrap`: lift onto a new row when the row would overflow
- `max_width`: a **number** (not a string) — cap the rendered width,
  ellipsised with `…`
- `min_cols`: a **number** — hide the segment when the terminal is narrower
- `shrink`: yield width first when the row would overflow
- `format`: per-type output format. `cwd`: `"full"` for the whole path instead
  of the basename. `cost`: a printf verb such as `"%.4f USD"`. `tty_size`: a
  template like `"{cols}c/{rows}r"`. `git_dirty`: a template such as `"{n}"`
  (default `"*{n}"`).

Two type-specific fields worth knowing:
- `scope` (`git_branch`): `"local"` (default) or `"toplevel"` — inside a
  submodule, `toplevel` reports the superproject's branch.
- `show_1m_flag` (`model`): appends a `1M` marker on 1M-context models.
  ccsb's shipped default sets this `true`, so a rewrite that omits it silently
  removes the marker.

**`max_width` and `shrink` truncate without understanding ANSI.** Use them
only on plain-text segments (`text`, `cwd`, `git_branch`, `model`,
`session_name`, …). Never set either on `context`, `limit_5h` or `limit_7d`
when that segment styles parts of itself — that is, when it uses
`threshold_target: "pct"`, `bar_fg`, `label_fg`, `bar_thresholds` or
`label_thresholds`. Cutting mid-escape leaves the terminal in a stray colour
state and prints the rest of the escape as literal text. `shrink` is the more
dangerous of the two, because nobody picks a width: it fires by itself as soon
as the terminal is narrow enough.

Percentage segments (`context`, `limit_5h`, `limit_7d`) additionally accept:
- `style`: `"pct"`, `"bar"`, or `"bar+pct"`. The default differs per type:
  `context` defaults to `"bar+pct"`, the two limit segments to `"pct"`. So
  writing `"style": "pct"` on a `context` segment is a real change, not a no-op.
- `bar_width`: bar length in cells (default 16)
- `bar_style`: `"circles"` (default, `○◔◑◕●`) or `"blocks"` (`░▏▎▍▌▋▊▉█`)
- `bar_glyphs`: an explicit ramp, e.g. `[".", ":", "#"]`, overriding `bar_style`
- `token_position`: `"before"`, `"after"` or `"hidden"` — where the token
  fraction sits relative to the bar (`context` only)
- `bar_fg`: colour the bar independently (all three types)
- `label_fg` / `label_thresholds`: colour the label independently — `limit_5h`
  and `limit_7d` only; `context` parses them and then ignores them
- `thresholds` / `bar_thresholds` / `label_thresholds`: an **array of objects**,
  `[{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]`, recolouring at those
  percentages. It is not a percentage→colour object: writing
  `{"70": "136"}` is a hard parse error that leaves you with no status bar.
- `threshold_target`: `"pct"` recolours only the digits, `"all"` the whole segment

The `version` segment additionally accepts:
- `check_update`: `true` enables a background check against GitHub for a
  newer release, appending `" (↑ v<latest>)"` when one exists. Defaults to
  `false` (the Go zero value) unless explicitly set — this is the one field
  where "the default" depends on which config you mean: ccsb's own shipped
  default layout sets it `true`, but any config the wizard writes or a user
  hand-edits starts with it off unless this key is present.

  **This key also gates `update.auto`.** The self-update trigger lives inside
  this segment's check, so a config whose `rows` contain no `version` segment
  with `check_update: true` never updates itself, no matter what `update.auto`
  says. Writing `rows` for a user who has `update.auto` set and omitting this
  key therefore switches their auto-updating off while leaving it configured —
  the setting survives, the behaviour does not. `ccsb doctor` reports exactly
  this case, which is how it usually gets noticed. So: if the config carries
  `update.auto`, give the layout a `version` segment with `check_update: true`.
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

Other keys of the `render` block:
- `margin`: leading blank columns on every row, default 2. This is the knob for
  "the bar is too wide" or "it gets clipped" — set `0` to reclaim both columns.
- `separator`: the string joining segments in **non**-Powerline rows, e.g.
  `" · "`. It belongs to the `render` block, **not** to a row and not to a
  segment — a `separator` key inside a row object is silently discarded. It has
  no effect in Powerline rows, which join with chevrons.

## Step 4: Generate and write the config

Produce the updated config JSON and apply it using the rule below.

| Situation | Action |
|---|---|
| No config existed | Write directly; confirm what was created |
| Small change (one segment, one or two colors) | Write directly; confirm what changed |
| Substantial rewrite (restructured rows, many color changes) | Show the proposed config; ask **"Shall I write this?"** then write on confirmation |

**Preserve the blocks you did not touch.** `config.json` holds four
top-level keys: `render` (yours to edit), plus `proxy`, `backup` and `update`,
which belong to ccsb:

- `backup.previous_status_line` is what `ccsb uninstall` restores the user's
  original statusLine from — lose it and their original bar is gone for good.
- `proxy` is managed by `ccsb mode`.
- `update.auto` (`"patch"`, `"minor"` or `"major"`) is the user's opt-in
  auto-update setting. Dropping it silently switches the feature off — and so
  does writing `rows` without a `version` segment that sets
  `check_update: true`, because that segment's check is what consults
  `update.auto` at all. Preserving the block is not enough; check the layout
  too.

So edit the file rather than replacing it. Read it first (Step 1), then use
the Edit tool to change only the `render` block, or rebuild the whole document
carrying `proxy`, `backup` and `update` over verbatim. Never `cat >` a document
that contains only `render`.

### A complete, valid config

This is what a full `config.json` looks like. Every value shape here is the
one ccsb actually accepts — copy the shapes, not necessarily the choices:

```json
{
  "proxy": {},
  "backup": {},
  "update": {
    "auto": "patch"
  },
  "render": {
    "powerline": true,
    "powerline_style": "solid",
    "cap_style": "round",
    "margin": 2,
    "separator": " · ",
    "palette": ["232", "233", "234", "235", "236", "237", "238", "239", "240"],
    "palette_stride": 2,
    "rows": [
      {
        "caps": true,
        "segments": [
          { "type": "model", "bold": true, "fg": "33", "show_1m_flag": true },
          { "type": "context", "style": "bar+pct", "bar_width": 16,
            "token_position": "after", "threshold_target": "pct",
            "thresholds": [{ "min": 70, "fg": "136" }, { "min": 90, "fg": "160" }] },
          { "type": "limit_5h", "label": "5h", "style": "pct",
            "label_thresholds": [{ "min": 80, "fg": "160" }] }
        ]
      },
      {
        "caps": true,
        "segments": [
          { "type": "git_branch", "scope": "local", "fg": "245" },
          { "type": "cwd", "format": "full", "shrink": true, "max_width": 40 },
          { "type": "version", "align": "right", "check_update": true,
            "update_check_interval": "24h", "update_minor_fg": "136",
            "update_major_fg": "208", "update_big_fg": "160" }
        ]
      }
    ]
  }
}
```

After writing, verify the config still parses — a malformed file means no
status bar at all:
```bash
ccsb status >/dev/null && echo "config OK"
```

Then say: **"Done. The next Claude Code status update will show the new
layout."**

Do not overwrite the existing config without the user's awareness. ccsb owns
its config; the wizard is a guest.

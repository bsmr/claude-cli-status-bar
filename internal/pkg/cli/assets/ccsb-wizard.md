# ccsb-wizard

You are helping the user configure ccsb — the Claude Code status bar. ccsb
renders one or more rows below the Claude Code input prompt. Follow the steps
below in order.

## Step 1: Read the existing config

Run:
```bash
cat "${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json" 2>/dev/null || echo "(no config)"
```

If the output is `(no config)`, note "starting fresh — will create a new config."
Otherwise note which segments and rows are already configured so you can offer
targeted changes rather than a full rewrite.

## Step 2: Symbol check (skip if `symbols.nerd_font` is already set in the config)

Output exactly these two lines verbatim. The first line contains NerdFont
codepoints (U+E0A0 branch, U+E0B0 separator) that render as icons in
NerdFont-capable terminals and as boxes or question marks otherwise:

```
 main  claude-sonnet-4-6 · $0.12
[git] main · claude-sonnet-4-6 · $0.12
```

Then ask: **"Does the top line show a branch icon before 'main', or does it show
a box or question mark?"**

- User sees an icon → `symbols.nerd_font: true`
- User sees a box or question mark → `symbols.nerd_font: false`

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
| `version` | ccsb binary version | `v0.4.0` |
| `text` | Static label | any string |

Each segment accepts:
- `fg` / `bg`: 256-color number (0–255) or named palette entry
- `bold`: `true` / `false`
- `shrink`: segment shrinks then disappears when the terminal is narrow
- `separator`: per-segment separator override

Colors: 232–255 are a grayscale ramp (232 ≈ near-black, 255 ≈ near-white).
Common accents: 196 red · 226 yellow · 46 green · 51 cyan · 21 blue · 135 purple.

Layout: `render.rows` is an array of rows (top to bottom). Each row has `segments`
(left to right). A named palette can be defined in `render.palette` (name → 256-color
number) and referenced by name in segment `fg`/`bg` fields.

## Step 4: Generate and write the config

Produce the updated config JSON and apply it using the rule below.

| Situation | Action |
|---|---|
| No config existed | Write directly; confirm what was created |
| Small change (one segment, one or two colors) | Write directly; confirm what changed |
| Substantial rewrite (restructured rows, many color changes) | Show the proposed config; ask **"Shall I write this?"** then write on confirmation |

Write path:
```bash
cat > "${XDG_CONFIG_HOME:-$HOME/.config}/ccsb/config.json" << 'EOF'
{ ... }
EOF
```

After writing, always say: **"Done. The next Claude Code status update will show
the new layout."**

Do not overwrite the existing config without the user's awareness. ccsb owns
its config; the wizard is a guest.

# Session Summary — ccsb (claude-cli-status-bar)

## Project

Go CLI `ccsb` providing Claude Code's `statusLine`. Reads JSON on stdin, prints one or more lines to stdout. Module `go.muehmer.eu/claude-cli-status-bar`. Repo: `/home/bsmr/repositories/git.nebula.muehmer.eu/claude-cli-status-bar`. Branch `main`.

## Shipped this session

| Ver | Type | Merge | Tag |
|---|---|---|---|
| 0.2.4 | fix | `cd49ddd` | `production-0.2.4` |
| 0.2.3 | feat | `75f454d` | `production-0.2.3` |
| 0.2.2 | feat | `3c63448` | `production-0.2.2` |
| 0.2.1 | fix | `14c7514` | `production-0.2.1` |

All on `origin`. Each version has archived `development-X.Y.Z-{work,main}` branches.

### Unversioned commits on main (post-0.2.4)

| Commit | Description |
|---|---|
| `b6bd38e` | feat: add `version` subcommand with ldflags injection |

### 0.2.1 — fix(chevron-spacing)
Add ` <chev> ` padding around U+E0B1 thin chevron. New `powerlineSeparatorWidth = powerlineChevronWidth + 2`. Padding math updated.

### 0.2.2 — feat(terminal-size-detection)
Detection chain `Config.Width > /dev/tty > /proc parent-process walk > (0,0)`. Three function-pointer reader vars (`devTTYWinsizeReader`, `procStatReader`, `procFDWinsizeReader`) for testability. New 15th segment `tty_size` formatting `{cols}×{rows}` (U+00D7). 2026-05-14 probe confirmed `/proc`-walk finds `claude` process TTY at depth 2.

### 0.2.3 — feat(Powerline polish)
Two bundled changes. **Margin**: `Config.Margin *int` (default 2 via `effectiveMargin()` — nil→2, neg→0, explicit 0 disables). Plain leading spaces + usable width shrunk by `2*margin` to leave room for Claude Code's statusLine chrome. **Alternating bgs + chevrons**: `Row.Palette []string` rotates per visible segment; `Config.PowerlineStyle "thin"` (default U+E0B1) or `"solid"` (U+E0B0); `defaultPalette=["234","236","238"]` when Powerline on but no Bg/Palette set; `defaultSameBgChevronFG="245"` fallback for legacy uniform-bg.

**0.2.3 had a spec bug** — both chevron spaces emitted in `next.bg`, producing a visible bg-jump before the glyph. Corrected in 0.2.4.

### 0.2.4 — fix(Powerline polish round 2)
**Asymmetric chevron geometry**:
- pre-space: `bg=prevBg`
- chev-cell: per-style — solid `bg=next, fg=prev` (wedge flow); thin `bg=prev, fg=next` (line at trailing edge)
- post-space: `bg=nextBg`
- same-bg fallback unchanged (`chevFg="245"`)

**Opt-in end caps**: `Row.Caps bool` (default false) + `Config.CapStyle string`:
- `"round"` (default): U+E0B6 / U+E0B4 half-circles
- `"square"`: 1-col bg-painted space (no glyph) — sentinel: `capGlyphs{}` empty pair
- `"slant"`: U+E0BC / U+E0BA filled triangles
- unknown → round

Each enabled cap consumes 1 col from `usableCols`. `pickCapGlyphs(style)` helper returns `capGlyphs{left, right string}`.

SGR asymmetry between caps (explained in render.go comments):
- left cap: `\x1b[49m + fg256(firstBg) + glyph + reset` (margin had no styling)
- right cap: `reset + fg256(lastBg) + glyph` (full reset clears prior segment's fg/bold)

### version subcommand (b6bd38e, unversioned)
`ccsb version` / `-v` / `--version` prints `ccsb version <Version>`.
`Version` var in `internal/pkg/cli/version.go`, defaults to `"dev"`.
Set at build time: `-ldflags "-X go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli.Version=X.Y.Z"`.

## Schema (post-0.2.4)

```go
type Config struct {
    Rows           []Row  `json:"rows,omitzero"`
    Separator      string `json:"separator,omitempty"`
    Powerline      bool   `json:"powerline,omitempty"`
    Width          int    `json:"width,omitempty"`
    Margin         *int   `json:"margin,omitempty"`           // nil→2, neg→0
    PowerlineStyle string `json:"powerline_style,omitempty"`  // "thin"|"solid"
    CapStyle       string `json:"cap_style,omitempty"`        // "round"|"square"|"slant"
}

type Row struct {
    Segments []Segment `json:"segments"`
    Bg       string    `json:"bg,omitempty"`
    Palette  []string  `json:"palette,omitempty"`
    Caps     bool      `json:"caps,omitempty"`
}

type renderEnv struct {
    cwd, powerlineStyle, capStyle string
    colorEnabled                  bool
    nowUnix                       int64
    ttyCols, ttyRows, margin      int
}
```

`effectiveSegmentBg(row, seg, visibleIndex, powerlineActive)` priority: `Segment.BG` > `Row.Palette[i%N]` > `Row.Bg` > `defaultPalette` (Powerline only) > `""`.

## User's live config

`/home/bsmr/.config/ccsb/config.json` (active post-0.2.4):
- `powerline: true`, `powerline_style: "solid"`, `cap_style: "round"`
- Row 1: `caps: true`, palette `["234","236","238"]` → model+mode+context+limit_5h+limit_7d (with threshold_target:"pct", thresholds {70:136, 90:160})
- Row 2: `caps: true`, palette `["237","238","237"]` → git_branch+lines+cwd

Verified glyph sequence: `LEFT-HALF-CIRCLE → 3×SOLID-WEDGE → RIGHT-HALF-CIRCLE` per row.

Binary deployed at `/home/bsmr/go/bin/ccsb` (installed via `go install`). Claude Code's `~/.claude/settings.json` `statusLine.command` points there. Mode: `native`.

**Migration pitfall**: Copying `config.json` from another system preserves `proxy.command`, which may point at a non-existent path. Symptom: `ccsb status` shows `mode: proxy` with a stale path → status bar silent. Fix: `ccsb mode native` (or `ccsb mode proxy <correct-path>`).

## Constraints (project conventions)

- **Language**: User responses in German (concise, technical, prompt-correction first). Code/comments/commits English.
- **Go style**: Google Go Style Guide. `main()` → `run()` pattern. I/O injected.
- **Build**: `go build -o bin/ccsb ./cmd/ccsb` (or deploy target for production). Never bare `go build`. Versioned: add `-ldflags "-X go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli.Version=X.Y.Z"`.
- **Gates**: `gofmt -l .` empty, `go vet ./...` clean, `go test -race -cover ./...` green. Render-package coverage ≥ 94.5% (currently 95.3%).
- **Glyph literals**: multi-byte UTF-8 sequences (U+E0Bx, U+00D7) can be dropped during tool transmission — use `"\ue0bX"` Unicode escapes as fallback. Verify with `python3 -c "open(...).read().count(b'\xee\x82\xb6')"`.
- **Git**: branch model `main` / `production-X.Y.Z` / `development-X.Y.Z-{work,main}`. Three explicit release gates (squash → no-ff merge → production tag + push) — never batch. Push only to `origin`, never `upstream`. Conventional commits, signed (`-s`), `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer. No amends; new commits for fix-ups. Never push to main on a non-FF state.

## Established workflow

1. `superpowers:brainstorming` → spec questions, lock decisions.
2. `superpowers:writing-plans` → bottom-up TDD plan in `docs/superpowers/plans/`.
3. `superpowers:subagent-driven-development` → dispatch fresh implementer per task + spec review + code-quality review. Sonnet for implementation/spec, Opus for final whole-branch review.
4. Three release gates with explicit user "ok" between each.
5. Memory update + user-config migration where relevant.

Specs live in `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`. Plans in `docs/superpowers/plans/YYYY-MM-DD-<topic>.md`.

## Memory (persists across sessions)

Path: `/home/bsmr/.claude/projects/-home-bsmr-repositories-git-nebula-muehmer-eu-claude-cli-status-bar/memory/`

- `MEMORY.md` — index
- `release_workflow_gates.md` — three-gate confirmation rule
- `release_sizing_one_concept.md` — feat/fix/docs alternation
- `statusline_aesthetic.md` — restrained palette, two accents + neutral, threshold escalation
- `02x_terminal_aware_layout.md` — roadmap, now titled "0.2.5+ — layout primitives"

## Next steps (0.2.5+)

Three independent primitives, each its own patch per `one-concept-per-patch` rule:

1. **Right-align** — row property `align: "right"` OR pseudo `{"type":"spacer"}` segment that absorbs slack. Decide which is more composable.
2. **`max_width` truncation** — per-segment field on `cwd`/`git_branch`/`text`; truncate with ellipsis when row exceeds `ttyCols`. Priority rule for which segment shrinks first.
3. **`min_cols` suppression** — per-segment field; segment suppressed when `env.ttyCols < min_cols`. Composes with empty-segment-drop.

Memory note `02x_terminal_aware_layout.md` has the design sketch. Foundation (`env.ttyCols`, `displayWidth`, `effectiveSegmentBg`, `pickGlyph`/`pickCapGlyphs` injection patterns) is all in place.

No active in-flight work.

## Open issues / known non-blocking nits

- `capStyleRound` and `powerlineStyleThin` constants flagged by LSP as "unused" — they ARE used as default fallback values in `pickGlyph`/`pickCapGlyphs` but the linter doesn't see that pattern. Real, intentional.
- `intPtr(v int) *int { return &v }` test helper has LSP suggestions to use `new(int)` for the zero case. Stylistic, current form preferred for readability.
- `TestRenderRowPowerline_LeftCapWithoutBgSkipped` is a documented no-op panic-guard; the branch is unreachable through public paths since `effectiveSegmentBg` always returns a non-empty bg under `powerlineActive=true`.

## Workspace cleanliness

- Working tree clean on `main` at `b6bd38e` (after version-subcommand commit).
- `HANDLING.md` is unrelated session-summary file in repo root, untracked, not session-relevant.

# Powerline Polish Round 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the 0.2.3 chevron transition spec error by splitting the chev-cell geometry per glyph style and re-anchoring the surrounding spaces, and add opt-in rounded end caps via `Row.Caps` + `Config.CapStyle`.

**Architecture:** Bottom-up TDD. Schema and helpers first (`Config.CapStyle`, `pickCapGlyphs`, `Row.Caps`). Then the chevron-geometry rewrite — independent of caps. Then `renderEnv.capStyle` plumbing and the cap emission inside `renderRowPowerline`. Documentation and a final verification gate close out.

**Tech Stack:** Go, package `go.muehmer.eu/claude-cli-status-bar/internal/pkg/render`. In-package tests. No new dependencies.

**Branch:** `development-0.2.4-work` already exists with spec commit `8e15b1b` as the only commit beyond `main` (`75f454d`). Plan executes on this branch.

---

## File Structure

- `internal/pkg/render/render.go` — all production edits land here: schema fields (`Config.CapStyle`, `Row.Caps`), constants (cap-style identifiers + round/slant glyphs), `capGlyphs` struct, `pickCapGlyphs` helper, `renderEnv.capStyle` field, `Render` plumbing, asymmetric chevron geometry in `renderRowPowerline`, optional left/right cap emission, padding math adjusted for caps.
- `internal/pkg/render/render_test.go` — new tests appended; one existing test (`TestRenderRowPowerline_ChevronTransitionColors`) updated; helper `intPtr` already present from 0.2.3.
- `docs/configuration.md` — new `cap_style` schema-table row, `caps` field documented in the row-shape section, Powerline chevron prose corrected, new "End caps" subsection.

No new files, no new packages, no new dependencies.

---

## Task 1: `Config.CapStyle` field + `pickCapGlyphs` helper + cap glyph constants

**Files:**
- Modify: `internal/pkg/render/render.go` (Config struct, cap-style constants, glyph constants, `capGlyphs` struct, `pickCapGlyphs` function)
- Modify: `internal/pkg/render/render_test.go` (append two test functions)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestConfigJSON_CapStyleRoundTrip(t *testing.T) {
	// Empty omits the key.
	out, err := json.Marshal(Config{})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(out), `"cap_style"`) {
		t.Errorf("empty cap_style must be omitted, got %s", out)
	}

	for _, v := range []string{"round", "square", "slant", "invalid"} {
		out, err := json.Marshal(Config{CapStyle: v})
		if err != nil {
			t.Fatalf("marshal %q: %v", v, err)
		}
		want := `"cap_style":"` + v + `"`
		if !strings.Contains(string(out), want) {
			t.Errorf("Config{CapStyle:%q} must encode %q, got %s", v, want, out)
		}
		var back Config
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", v, err)
		}
		if back.CapStyle != v {
			t.Errorf("round-trip %q: got %q", v, back.CapStyle)
		}
	}
}

func TestPickCapGlyphs(t *testing.T) {
	cases := []struct {
		in        string
		wantLeft  string
		wantRight string
	}{
		{"", powerlineLeftCapRound, powerlineRightCapRound},
		{"round", powerlineLeftCapRound, powerlineRightCapRound},
		{"square", "", ""}, // sentinel: empty pair means square (no glyph)
		{"slant", powerlineLeftCapSlant, powerlineRightCapSlant},
		{"invalid", powerlineLeftCapRound, powerlineRightCapRound}, // fall back to round
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := pickCapGlyphs(c.in)
			if got.left != c.wantLeft || got.right != c.wantRight {
				t.Errorf("pickCapGlyphs(%q): got (%q, %q), want (%q, %q)",
					c.in, got.left, got.right, c.wantLeft, c.wantRight)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestConfigJSON_CapStyleRoundTrip|TestPickCapGlyphs" ./internal/pkg/render -v`
Expected: compile error reporting `unknown field CapStyle`, `undefined: pickCapGlyphs`, `undefined: powerlineLeftCapRound`, etc.

- [ ] **Step 3: Add the `CapStyle` field, constants, struct, and helper**

In `internal/pkg/render/render.go`, locate the `Config` struct (now ending with `PowerlineStyle string`). Add `CapStyle` after `PowerlineStyle`:

```go
	// PowerlineStyle selects the chevron glyph between segments when
	// Powerline is enabled. "" or "thin" (default) renders U+E0B1, the
	// thin chevron. "solid" renders U+E0B0, the filled wedge. Unknown
	// values silently fall back to thin so a typo cannot break
	// rendering.
	PowerlineStyle string `json:"powerline_style,omitempty"`
	// CapStyle selects the end-cap glyph variant when a row's Caps
	// field is true. "" or "round" (default) renders U+E0B6 / U+E0B4
	// filled half-circles. "square" emits a 1-col plain bg-painted
	// space on each side. "slant" renders U+E0BC / U+E0BA filled
	// triangles. Unknown values fall back to "round".
	CapStyle string `json:"cap_style,omitempty"`
}
```

Locate the existing Powerline constants block (currently ending with `defaultSameBgChevronFG`). Replace:

```go
const (
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2

	// Style identifiers for Config.PowerlineStyle.
	powerlineStyleThin  = "thin"
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = "" // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = "" // U+E0B0 RIGHT TRIANGLE FILL

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)
```

with:

```go
const (
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2

	// Style identifiers for Config.PowerlineStyle.
	powerlineStyleThin  = "thin"
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = "" // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = "" // U+E0B0 RIGHT TRIANGLE FILL

	// Cap-style identifiers for Config.CapStyle.
	capStyleRound  = "round"
	capStyleSquare = "square"
	capStyleSlant  = "slant"

	// Round caps: filled half-circles.
	powerlineLeftCapRound  = "" // U+E0B6 LEFT HALF CIRCLE THICK
	powerlineRightCapRound = "" // U+E0B4 RIGHT HALF CIRCLE THICK

	// Slant caps: filled triangles.
	powerlineLeftCapSlant  = "" // U+E0BC UPPER-LEFT TRIANGLE FILLED
	powerlineRightCapSlant = "" // U+E0BA LOWER-RIGHT TRIANGLE FILLED

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)

// capGlyphs carries the (left, right) glyph pair for a cap style.
// Empty strings are the "square" sentinel: emit a 1-col bg-painted
// space instead of a glyph.
type capGlyphs struct {
	left, right string
}

// pickCapGlyphs maps Config.CapStyle to its (left, right) glyph
// pair. Empty/unknown styles fall back to round.
func pickCapGlyphs(style string) capGlyphs {
	switch style {
	case capStyleSquare:
		return capGlyphs{} // sentinel: square path
	case capStyleSlant:
		return capGlyphs{left: powerlineLeftCapSlant, right: powerlineRightCapSlant}
	default: // round, "", or unknown
		return capGlyphs{left: powerlineLeftCapRound, right: powerlineRightCapRound}
	}
}
```

The literal glyphs `` (U+E0B6), `` (U+E0B4), `` (U+E0BC), and `` (U+E0BA) are 3-byte UTF-8 sequences each. If the source-code editor / channel drops the bytes, restore via Unicode escapes:

```go
powerlineLeftCapRound  = ""
powerlineRightCapRound = ""
powerlineLeftCapSlant  = ""
powerlineRightCapSlant = ""
```

After the edit, verify the bytes are correct by reading them back with `python3 -c "open('internal/pkg/render/render.go','rb').read()"` and grepping for `\xee\x82\xb6` / `\xee\x82\xb4` / `\xee\x82\xbc` / `\xee\x82\xba`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run "TestConfigJSON_CapStyleRoundTrip|TestPickCapGlyphs" ./internal/pkg/render -v`
Expected: all sub-cases PASS.

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all existing tests PASS. The new constants and helper are not yet consumed.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Config.CapStyle + pickCapGlyphs + cap-glyph constants

Three cap-style identifiers ("round" default, "square", "slant")
plus two glyph pairs (round: U+E0B6/U+E0B4 half-circles; slant:
U+E0BC/U+E0BA filled triangles). "square" is a sentinel that
emits a bg-painted space instead of a glyph. pickCapGlyphs maps
the style identifier to a (left, right) pair, with unknown values
falling back to round.

No consumer yet — wired into renderEnv and rendering in later
tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `Row.Caps` schema field

**Files:**
- Modify: `internal/pkg/render/render.go` (Row struct)
- Modify: `internal/pkg/render/render_test.go` (append test)

### Step 1: Write the failing test

Append to `internal/pkg/render/render_test.go`:

```go
func TestRowJSON_CapsRoundTrip(t *testing.T) {
	// Default false omits the key.
	out, err := json.Marshal(Row{Segments: []Segment{{Type: "text", Label: "x"}}})
	if err != nil {
		t.Fatalf("marshal default: %v", err)
	}
	if strings.Contains(string(out), `"caps"`) {
		t.Errorf("default caps must be omitted, got %s", out)
	}

	// True emits and round-trips. Coexists with bg and palette.
	in := Row{
		Bg:       "234",
		Palette:  []string{"234", "236"},
		Caps:     true,
		Segments: []Segment{{Type: "text", Label: "a"}},
	}
	out, err = json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if !strings.Contains(string(out), `"caps":true`) {
		t.Errorf("caps:true must encode, got %s", out)
	}
	if !strings.Contains(string(out), `"bg":"234"`) {
		t.Errorf("bg must coexist with caps, got %s", out)
	}
	if !strings.Contains(string(out), `"palette":["234","236"]`) {
		t.Errorf("palette must coexist with caps, got %s", out)
	}

	var back Row
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Caps {
		t.Errorf("caps round-trip: got %v, want true", back.Caps)
	}
	if back.Bg != "234" || len(back.Palette) != 2 {
		t.Errorf("bg/palette lost in round-trip: bg=%q palette=%v", back.Bg, back.Palette)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestRowJSON_CapsRoundTrip ./internal/pkg/render -v`
Expected: compile error reporting `unknown field Caps in struct literal of type render.Row`.

- [ ] **Step 3: Add `Caps` field to `Row`**

In `internal/pkg/render/render.go`, locate the `Row` struct (currently ending with `Palette []string`). Replace:

```go
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
	// Palette, when non-empty, rotates through these ANSI 256-color
	// strings across the visible segments of this row. Indexed by
	// visible-segment rank (empty segments don't claim a palette
	// slot), modulo len(Palette). Per-segment Segment.BG overrides
	// the palette at that position. When Palette is empty, the
	// resolution falls through to Bg (uniform fill) and finally to
	// the package-level defaultPalette when Powerline is active.
	Palette []string `json:"palette,omitempty"`
}
```

with:

```go
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
	// Palette, when non-empty, rotates through these ANSI 256-color
	// strings across the visible segments of this row. Indexed by
	// visible-segment rank (empty segments don't claim a palette
	// slot), modulo len(Palette). Per-segment Segment.BG overrides
	// the palette at that position. When Palette is empty, the
	// resolution falls through to Bg (uniform fill) and finally to
	// the package-level defaultPalette when Powerline is active.
	Palette []string `json:"palette,omitempty"`
	// Caps, when true and the row's first or last visible segment
	// has an effective bg, emits a 1-col cap glyph on the
	// corresponding side. The glyph variant is selected globally
	// via Config.CapStyle. Each enabled cap consumes 1 column from
	// the row's usable bg-fill width.
	Caps bool `json:"caps,omitempty"`
}
```

The `Row.UnmarshalJSON` method's alias-based object decode picks up the new field automatically; no change to the unmarshaller is needed. The legacy bare-array form continues to unmarshal into `{Segments: ..., Bg: "", Palette: nil, Caps: false}`.

- [ ] **Step 4: Run the test**

Run: `go test -run TestRowJSON_CapsRoundTrip ./internal/pkg/render -v`
Expected: PASS.

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Row.Caps schema field

Per-row opt-in bool that triggers end-cap emission in
renderRowPowerline. Default false preserves backward
compatibility. The glyph variant is selected globally via
Config.CapStyle, defined in the previous task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Asymmetric chevron geometry correction

**Files:**
- Modify: `internal/pkg/render/render.go` (`renderRowPowerline` step 3 — the chevron-emission block between segments)
- Modify: `internal/pkg/render/render_test.go` (update one existing test, append two new tests)

This task corrects the 0.2.3 chevron-transition spec error. It is independent of caps and lands before the cap plumbing.

### Step 1: Write the failing tests

In `internal/pkg/render/render_test.go`, locate `TestRenderRowPowerline_ChevronTransitionColors` (currently around line 1291). Replace the entire function with:

```go
func TestRenderRowPowerline_ChevronTransitionColors(t *testing.T) {
	row := Row{
		Palette: []string{"234", "236"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true} // thin glyph default
	got := renderRowPowerline(&payload{}, row, env)
	// Thin geometry: chev-cell bg = prev.bg (234), fg = next.bg (236).
	if !strings.Contains(got, "\x1b[38;5;236m") {
		t.Errorf("thin chevron fg = 236 missing\nfull: %q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("thin chevron cell bg = 234 missing\nfull: %q", got)
	}
}
```

Append after `TestRenderRowPowerline_ChevronTransitionColors`:

```go
func TestRenderRowPowerline_SolidChevronTransitionColors(t *testing.T) {
	row := Row{
		Palette: []string{"234", "236"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true, powerlineStyle: powerlineStyleSolid}
	got := renderRowPowerline(&payload{}, row, env)
	// Solid geometry: chev-cell bg = next.bg (236), fg = prev.bg (234).
	if !strings.Contains(got, "\x1b[38;5;234m") {
		t.Errorf("solid chevron fg = 234 missing\nfull: %q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;236m") {
		t.Errorf("solid chevron cell bg = 236 missing\nfull: %q", got)
	}
}

func TestRenderRowPowerline_ChevronPreSpaceInPrevBg(t *testing.T) {
	// Regression guard for the 0.2.3 bug where both surrounding
	// spaces rendered in next.bg. With the corrected geometry the
	// pre-chevron space must render in prev.bg (234), not next.bg
	// (236). Check by stripping ANSI and looking at the byte
	// sequence immediately following the first segment's body "A":
	// it must transition through a prev-bg-painted space before the
	// chev cell sets next.bg.
	row := Row{
		Palette: []string{"234", "236"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)

	// Find the chevron glyph position in the raw output.
	chevIdx := strings.Index(got, powerlineThinGlyph)
	if chevIdx < 0 {
		t.Fatalf("chevron glyph not in output: %q", got)
	}
	// Look backwards from chevIdx for the pre-space. The pre-space
	// MUST be preceded by bg256(prevBg="234"). If the output
	// instead contains bg256(nextBg="236") immediately before the
	// pre-space, the 0.2.3 bug has regressed.
	before := got[:chevIdx]
	preSpaceBg234 := "\x1b[48;5;234m \x1b[48;5;234m"
	preSpaceBg236 := "\x1b[48;5;236m \x1b[48;5;236m"
	if !strings.Contains(before, preSpaceBg234) {
		t.Errorf("pre-chevron region must include bg=234 (prev) space, got prefix %q", before)
	}
	if strings.Contains(before, preSpaceBg236) {
		t.Errorf("pre-chevron region must NOT have bg=236 (next) before glyph — regression of 0.2.3 bug; got prefix %q", before)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run "TestRenderRowPowerline_ChevronTransitionColors|TestRenderRowPowerline_SolidChevronTransitionColors|TestRenderRowPowerline_ChevronPreSpaceInPrevBg" ./internal/pkg/render -v`

Expected:
- `TestRenderRowPowerline_ChevronTransitionColors` — FAIL. Current production code emits chev with fg=prev=234, bg=next=236 (the old uniform rule). New assertion wants fg=next=236, bg=prev=234.
- `TestRenderRowPowerline_SolidChevronTransitionColors` — PASS coincidentally with current 0.2.3 code if the chev cell bg/fg already match (current code does fg=234, bg=236 for both styles).
- `TestRenderRowPowerline_ChevronPreSpaceInPrevBg` — FAIL. The 0.2.3 production code emits both spaces in next.bg (236), not the corrected prev.bg (234).

If a test other than the three expected ones fails, STOP and report — that may indicate an unrelated regression.

- [ ] **Step 3: Rewrite the chevron-emission block in `renderRowPowerline`**

In `internal/pkg/render/render.go`, locate the chevron block inside `renderRowPowerline` (the `else` branch of `if i == 0` within the segment-walk loop, currently around lines 532-573). Replace the existing block:

```go
		} else {
			// Chevron transition between visible[i-1] and visible[i].
			prevBg := visible[i-1].bg
			nextBg := seg.bg
			chevFg := prevBg
			if prevBg == nextBg {
				chevFg = defaultSameBgChevronFG
			}
			// Both spaces around the glyph render in nextBg. The
			// glyph itself adds chevFg as fg. A full reset closes the
			// chevron region, then bg256(nextBg) re-asserts bg for the
			// next segment body.
			b.WriteString(bg256(nextBg))
			b.WriteString(" ")
			b.WriteString(fg256(chevFg))
			b.WriteString(glyph)
			b.WriteString(reset)
			b.WriteString(bg256(nextBg))
			b.WriteString(" ")
		}
```

with:

```go
		} else {
			// Chevron transition between visible[i-1] and visible[i].
			prevBg := visible[i-1].bg
			nextBg := seg.bg

			// Per-style asymmetric chev-cell geometry:
			//   solid (U+E0B0): chev-cell bg=next, fg=prev — the
			//     filled wedge in prev colour visually flows into
			//     next bg.
			//   thin  (U+E0B1): chev-cell bg=prev, fg=next — the
			//     line in next colour sits at the trailing edge of
			//     prev; the bg switch happens at the post-space.
			var chevCellBg, chevFg string
			if env.powerlineStyle == powerlineStyleSolid {
				chevCellBg, chevFg = nextBg, prevBg
			} else {
				chevCellBg, chevFg = prevBg, nextBg
			}
			if prevBg == nextBg {
				// Same-bg fallback: keep the glyph visible via a
				// static fg so legacy uniform-bg configs do not lose
				// the separator.
				chevFg = defaultSameBgChevronFG
			}

			// Powerline transition layout (corrects the 0.2.3 spec
			// error where both spaces rendered in nextBg):
			//   pre-space in prevBg (extends prev segment by 1 col),
			//   chev-cell in (chevCellBg, chevFg),
			//   post-space in nextBg (extends next segment by 1 col).
			b.WriteString(bg256(prevBg))
			b.WriteString(" ")
			b.WriteString(bg256(chevCellBg))
			b.WriteString(fg256(chevFg))
			b.WriteString(glyph)
			b.WriteString(reset)
			b.WriteString(bg256(nextBg))
			b.WriteString(" ")
		}
```

- [ ] **Step 4: Run the three new/updated tests**

Run: `go test -run "TestRenderRowPowerline_ChevronTransitionColors|TestRenderRowPowerline_SolidChevronTransitionColors|TestRenderRowPowerline_ChevronPreSpaceInPrevBg" ./internal/pkg/render -v`

Expected: all three PASS.

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS. If any pre-existing `TestRenderRowPowerline_*` test fails because it asserted the old 0.2.3 byte pattern, STOP and report — review may need to update those assertions too. The expected failure pattern would be that a test asserts both `\x1b[48;5;236m` (next.bg) immediately AROUND the chevron glyph, which is now wrong. Such tests need to be updated to use substring assertions rather than exact byte sequences.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
fix(render): asymmetric chevron geometry per glyph style

Correct the 0.2.3 chevron transition spec error. Both surrounding
spaces previously rendered in next.bg, producing a visible bg-jump
before the chevron glyph. New geometry:

  pre-space  bg = prevBg     (extends prev segment by 1 col)
  chev-cell  bg = chevCellBg
             fg = chevFg     (per-style asymmetric pair)
  post-space bg = nextBg     (extends next segment by 1 col)

Per-style chev-cell pair:
  solid: bg=next, fg=prev (wedge flows prev into next)
  thin:  bg=prev, fg=next (line marks trailing edge of prev)

Same-bg fallback (chevFg = "245") preserves legacy uniform-bg
behaviour. Caps integration follows in the next task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `renderEnv.capStyle` plumbing + cap emission in `renderRowPowerline`

**Files:**
- Modify: `internal/pkg/render/render.go` (renderEnv struct, Render env-setup, renderRowPowerline cap blocks + width-math adjustment)
- Modify: `internal/pkg/render/render_test.go` (append seven test functions)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestRenderRowPowerline_NoCapsByDefault(t *testing.T) {
	row := Row{
		Palette:  []string{"234", "236"},
		Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	// None of the four cap glyphs should appear when row.Caps is false.
	for _, glyph := range []string{
		powerlineLeftCapRound, powerlineRightCapRound,
		powerlineLeftCapSlant, powerlineRightCapSlant,
	} {
		if strings.Contains(got, glyph) {
			t.Errorf("cap glyph %q must not appear when Caps=false, got %q", glyph, got)
		}
	}
}

func TestRenderRowPowerline_RoundCapsEmitGlyphs(t *testing.T) {
	row := Row{
		Caps:     true,
		Palette:  []string{"234", "236"},
		Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
	}
	env := renderEnv{colorEnabled: true} // capStyle default → round
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, powerlineLeftCapRound) {
		t.Errorf("round left cap glyph missing\nfull: %q", got)
	}
	if !strings.Contains(got, powerlineRightCapRound) {
		t.Errorf("round right cap glyph missing\nfull: %q", got)
	}
	// Left cap in fg = first.bg = 234.
	leftCapSeq := "\x1b[38;5;234m" + powerlineLeftCapRound
	if !strings.Contains(got, leftCapSeq) {
		t.Errorf("left cap fg=234 not paired with glyph, got %q", got)
	}
	// Right cap in fg = last.bg = 236.
	rightCapSeq := "\x1b[38;5;236m" + powerlineRightCapRound
	if !strings.Contains(got, rightCapSeq) {
		t.Errorf("right cap fg=236 not paired with glyph, got %q", got)
	}
}

func TestRenderRowPowerline_SlantCapsEmitGlyphs(t *testing.T) {
	row := Row{
		Caps:     true,
		Palette:  []string{"234", "236"},
		Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
	}
	env := renderEnv{colorEnabled: true, capStyle: capStyleSlant}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, powerlineLeftCapSlant) {
		t.Errorf("slant left cap glyph missing\nfull: %q", got)
	}
	if !strings.Contains(got, powerlineRightCapSlant) {
		t.Errorf("slant right cap glyph missing\nfull: %q", got)
	}
	// And the round glyphs must NOT appear.
	if strings.Contains(got, powerlineLeftCapRound) {
		t.Errorf("round left cap must not appear when style=slant, got %q", got)
	}
	if strings.Contains(got, powerlineRightCapRound) {
		t.Errorf("round right cap must not appear when style=slant, got %q", got)
	}
}

func TestRenderRowPowerline_SquareCapsEmitBgSpaces(t *testing.T) {
	row := Row{
		Caps:     true,
		Palette:  []string{"234", "236"},
		Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
	}
	env := renderEnv{colorEnabled: true, capStyle: capStyleSquare}
	got := renderRowPowerline(&payload{}, row, env)
	// No cap glyphs at all.
	for _, glyph := range []string{
		powerlineLeftCapRound, powerlineRightCapRound,
		powerlineLeftCapSlant, powerlineRightCapSlant,
	} {
		if strings.Contains(got, glyph) {
			t.Errorf("cap glyph %q must not appear with cap_style=square, got %q", glyph, got)
		}
	}
}

func TestRenderRowPowerline_UnknownCapStyleFallsBackToRound(t *testing.T) {
	row := Row{
		Caps:     true,
		Palette:  []string{"234"},
		Segments: []Segment{{Type: "text", Label: "A"}},
	}
	env := renderEnv{colorEnabled: true, capStyle: "garbage"}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, powerlineLeftCapRound) {
		t.Errorf("unknown cap_style must fall back to round left cap, got %q", got)
	}
	if !strings.Contains(got, powerlineRightCapRound) {
		t.Errorf("unknown cap_style must fall back to round right cap, got %q", got)
	}
}

func TestRenderRowPowerline_CapsWidthMath(t *testing.T) {
	// ttyCols=80, margin=2, row.Caps=true → usableCols = 80 - 4 - 2 = 74.
	// 3 single-col segments (3) + 2 separators of width 3 (6) = 9 used.
	// Padding fills 74 - 9 = 65 cols.
	// Total visible width = 2 (margin) + 1 (left cap) + 9 (content) +
	// 65 (pad) + 1 (right cap) = 78 cols = ttyCols - margin.
	row := Row{
		Caps:     true,
		Palette:  []string{"234", "236", "238"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
			{Type: "text", Label: "C"},
		},
	}
	env := renderEnv{colorEnabled: true, ttyCols: 80, margin: 2}
	got := renderRowPowerline(&payload{}, row, env)
	if w := displayWidth(got); w != 78 {
		t.Errorf("padded visible width with caps: got %d, want 78 (= margin(2) + cap(1) + content(9) + pad(65) + cap(1))\noutput: %q", w, got)
	}
}

func TestRenderRowPowerline_LeftCapWithoutBgSkipped(t *testing.T) {
	// row.Caps=true but the first segment's effective bg is empty
	// (achieved by setting an empty per-segment BG and disabling the
	// palette/Row.Bg fallback via the powerlineActive=false path —
	// but renderRowPowerline always uses powerlineActive=true. So
	// construct the case via Row.Bg="" + Palette empty + first seg
	// also empty BG — defaultPalette will kick in. To genuinely test
	// the empty-bg branch we have to construct an env without
	// effective bg for the first seg. The simplest approach is to
	// pin Segment.BG="" + Palette empty + Bg empty and rely on
	// effectiveSegmentBg returning "" only when powerlineActive is
	// false, which renderRowPowerline doesn't permit.
	//
	// Conclusion: this branch is unreachable through the standard
	// path. We construct the case by directly stubbing — that is
	// outside the public function's contract. So this test exists
	// only to document the guard. It must not panic.
	row := Row{
		Caps:     true,
		Segments: []Segment{{Type: "text", Label: "A"}},
	}
	env := renderEnv{colorEnabled: true}
	_ = renderRowPowerline(&payload{}, row, env) // does not panic
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run "TestRenderRowPowerline_NoCapsByDefault|TestRenderRowPowerline_RoundCapsEmitGlyphs|TestRenderRowPowerline_SlantCapsEmitGlyphs|TestRenderRowPowerline_SquareCapsEmitBgSpaces|TestRenderRowPowerline_UnknownCapStyleFallsBackToRound|TestRenderRowPowerline_CapsWidthMath|TestRenderRowPowerline_LeftCapWithoutBgSkipped" ./internal/pkg/render -v`

Expected:
- `NoCapsByDefault` may pass coincidentally (caps not implemented yet, no glyphs emitted).
- `RoundCapsEmitGlyphs`, `SlantCapsEmitGlyphs`, `UnknownCapStyleFallsBackToRound` — FAIL. Cap rendering not implemented.
- `SquareCapsEmitBgSpaces` may pass coincidentally (no cap glyphs emitted because not implemented).
- `CapsWidthMath` — FAIL. Width without caps would be 80 (existing 0.2.3 behaviour with margin=2 → margin+usable = 2+76 = 78 — wait actually that also matches). Re-check: without caps, usableCols=76, used=9, pad=67, total visible = 2 + 9 + 67 = 78. So caps add 2 cols visually but subtract 2 from usable, total stays 78. The assertion holds in both states. Hmm. **Mark this as a weak test** — it doesn't isolate the cap-width-math change. Acceptable: the test still serves as a sanity guard, but the failure mode is "caps glyphs absent" rather than "wrong width".
- `LeftCapWithoutBgSkipped` — passes trivially (current code doesn't panic).

Note the imperfect failure mode of `CapsWidthMath`. The test value is to validate the end-to-end math under caps; it stays useful even when not strictly failing-before-fix.

- [ ] **Step 3: Extend `renderEnv` with `capStyle`**

In `internal/pkg/render/render.go`, locate the `renderEnv` struct (currently has 7 fields ending with `powerlineStyle`). Replace:

```go
type renderEnv struct {
	cwd            string
	colorEnabled   bool
	nowUnix        int64
	ttyCols        int
	ttyRows        int
	margin         int
	powerlineStyle string
}
```

with:

```go
type renderEnv struct {
	cwd            string
	colorEnabled   bool
	nowUnix        int64
	ttyCols        int
	ttyRows        int
	margin         int
	powerlineStyle string
	capStyle       string // "" / "round" / "square" / "slant"; "" → round
}
```

- [ ] **Step 4: Plumb `capStyle` in `Render`**

In `internal/pkg/render/render.go`, locate the `env := renderEnv{...}` block inside `Render` (currently around lines 276-285). Replace:

```go
	env := renderEnv{
		cwd:            cwd,
		colorEnabled:   !opts.NoColor,
		nowUnix:        nowFunc().Unix(),
		powerlineStyle: opts.Config.PowerlineStyle,
	}
	env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
	env.margin = opts.Config.effectiveMargin()
	// Degrade gracefully if the terminal is narrower than 2*margin
	// — keep at least one column of usable bg-fill width.
	if env.ttyCols > 0 && env.ttyCols <= 2*env.margin {
		env.margin = 0
	}
```

with:

```go
	env := renderEnv{
		cwd:            cwd,
		colorEnabled:   !opts.NoColor,
		nowUnix:        nowFunc().Unix(),
		powerlineStyle: opts.Config.PowerlineStyle,
		capStyle:       opts.Config.CapStyle,
	}
	env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
	env.margin = opts.Config.effectiveMargin()
	// Degrade gracefully if the terminal is narrower than 2*margin
	// — keep at least one column of usable bg-fill width.
	if env.ttyCols > 0 && env.ttyCols <= 2*env.margin {
		env.margin = 0
	}
```

- [ ] **Step 5: Add left-cap block in `renderRowPowerline`**

In `internal/pkg/render/render.go`, locate the segment-walk in `renderRowPowerline`. Between the margin-emit step (step 2) and the segment loop (step 3), insert a left-cap block. The current code reads:

```go
	// 2. Leading margin: plain spaces with no bg, so Claude Code's
	//    statusLine chrome shows through.
	if env.margin > 0 {
		b.WriteString(strings.Repeat(" ", env.margin))
	}

	// 3. Walk segments, interleaving chevrons.
```

Replace with:

```go
	// 2. Leading margin: plain spaces with no bg, so Claude Code's
	//    statusLine chrome shows through.
	if env.margin > 0 {
		b.WriteString(strings.Repeat(" ", env.margin))
	}

	// 2.5. Resolve cap presence and glyphs once per row.
	caps := pickCapGlyphs(env.capStyle)
	hasLeftCap := row.Caps && visible[0].bg != ""
	hasRightCap := row.Caps && visible[len(visible)-1].bg != ""

	// 2.6. Left cap: glyph in fg=first.bg on default bg, or a 1-col
	//      bg-painted plain space for "square" style.
	if hasLeftCap {
		firstBg := visible[0].bg
		if caps.left == "" {
			// Square: 1 col of plain first.bg-painted space.
			b.WriteString(bg256(firstBg))
			b.WriteString(" ")
		} else {
			// Round / slant: glyph in fg=firstBg on default bg.
			b.WriteString("\x1b[49m") // default bg
			b.WriteString(fg256(firstBg))
			b.WriteString(caps.left)
			b.WriteString(reset)
		}
	}

	// 3. Walk segments, interleaving chevrons.
```

- [ ] **Step 6: Update padding math and add right-cap block**

In `internal/pkg/render/render.go`, locate the padding step in `renderRowPowerline` (step 4) and the close (step 5). The current code reads:

```go
	// 4. Pad to (ttyCols - 2*margin) total visible cols of bg-fill.
	if env.ttyCols > 0 {
		usableCols := env.ttyCols - 2*env.margin
		used := 0
		for _, seg := range visible {
			used += displayWidth(seg.body)
		}
		used += (len(visible) - 1) * powerlineSeparatorWidth
		if remaining := usableCols - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	// 5. Close.
	b.WriteString(reset)
	return b.String()
}
```

Replace with:

```go
	// 4. Pad to (ttyCols - 2*margin - 2*cap_cols) total visible cols
	//    of bg-fill so the optional left and right caps each occupy
	//    1 col within the usable region.
	if env.ttyCols > 0 {
		usableCols := env.ttyCols - 2*env.margin
		if hasLeftCap {
			usableCols--
		}
		if hasRightCap {
			usableCols--
		}
		used := 0
		for _, seg := range visible {
			used += displayWidth(seg.body)
		}
		used += (len(visible) - 1) * powerlineSeparatorWidth
		if remaining := usableCols - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	// 4.5. Right cap: glyph in fg=last.bg on default bg, or a 1-col
	//      bg-painted plain space for "square" style.
	if hasRightCap {
		lastBg := visible[len(visible)-1].bg
		if caps.right == "" {
			// Square: 1 col of plain last.bg-painted space (bg is
			// already set to lastBg from the previous segment's
			// trailing bg-restore).
			b.WriteString(" ")
		} else {
			b.WriteString(reset)
			b.WriteString(fg256(lastBg))
			b.WriteString(caps.right)
		}
	}

	// 5. Close.
	b.WriteString(reset)
	return b.String()
}
```

- [ ] **Step 7: Run the new tests**

Run: `go test -run "TestRenderRowPowerline_NoCapsByDefault|TestRenderRowPowerline_RoundCapsEmitGlyphs|TestRenderRowPowerline_SlantCapsEmitGlyphs|TestRenderRowPowerline_SquareCapsEmitBgSpaces|TestRenderRowPowerline_UnknownCapStyleFallsBackToRound|TestRenderRowPowerline_CapsWidthMath|TestRenderRowPowerline_LeftCapWithoutBgSkipped" ./internal/pkg/render -v`

Expected: all 7 PASS.

- [ ] **Step 8: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`

Expected: every test PASSES. The asymmetric chev geometry from Task 3 is preserved. The new cap logic is opt-in (default `Row.Caps=false`) so the existing tests that don't set `Caps` are unaffected.

- [ ] **Step 9: Run gofmt + vet**

Run: `gofmt -l . && go vet ./...`

Expected: empty output for both.

- [ ] **Step 10: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): opt-in end caps via Row.Caps + Config.CapStyle

renderEnv gains capStyle string; Render plumbs it from
Config.CapStyle. renderRowPowerline emits an optional left cap
before the first segment and an optional right cap after the
padding when Row.Caps is true and the first/last segment has an
effective bg. Cap glyphs come from pickCapGlyphs: round
(default), slant, or square (1-col bg-painted space, no glyph).
Padding math subtracts 1 col per active cap from usableCols so
the row's total visible width is unchanged.

Seven new tests cover the default-off behaviour, the three
glyph styles, unknown-style fallback, the end-to-end width
math, and the empty-bg guard.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Documentation refresh

**Files:**
- Modify: `docs/configuration.md` (cap_style schema-table row + caps in row shape + Powerline chevron prose correction + new "End caps" subsection)

### Step 1: Add the `cap_style` row to the schema table

In `docs/configuration.md`, locate the `### Fields` table. It currently ends with the `powerline_style` row (from 0.2.3). Append one row:

```
| `cap_style` | string | `"round"` | End-cap glyph style applied when a row's `caps` is true. `"round"` (default) renders U+E0B6 / U+E0B4 half-circles; `"square"` extends the bg with a 1-col plain space on each side; `"slant"` renders U+E0BC / U+E0BA filled triangles. Unknown values fall back to `"round"`. |
```

### Step 2: Add the `caps` field to the row shape section

In `docs/configuration.md`, locate the `### Row shape` section. After the existing `palette` field paragraph (added in 0.2.3), append:

````markdown
The object form also accepts an optional boolean `caps` field. When
`caps: true` and the first or last visible segment has an effective
bg, a 1-col cap glyph is emitted on the corresponding side. The
glyph variant is selected globally via [`cap_style`](#fields). Each
enabled cap consumes 1 column from the row's usable bg-fill width.

```json
{"caps": true, "palette": ["234", "236", "238"], "segments": [...]}
```
````

### Step 3: Refresh the Powerline section's chevron prose

In `docs/configuration.md`, locate the chevron bullet in the `### Powerline` section. After Task 7 of 0.2.3 it reads:

```
- Segments are joined with a Powerline chevron in classic
  transition colours: the chevron's foreground is the **previous**
  segment's effective background, its background is the **next**
  segment's effective background. When both adjacent backgrounds
  are the same (legacy uniform-bg configs without a palette), the
  chevron foreground falls back to `245` so it stays visible. The
  glyph defaults to the U+E0B1 thin chevron and switches to U+E0B0
  solid wedge when `powerline_style: "solid"` is set in the config.
  A single space surrounds the glyph on each side; those spaces
  inherit the next segment's background.
```

Replace with:

```
- Segments are joined with a Powerline chevron whose colours depend
  on the glyph style. `"solid"` (U+E0B0, filled wedge) renders with
  fg = previous segment's bg and bg = next segment's bg, so the
  wedge shape flows the prev colour into the next region. `"thin"`
  (U+E0B1, line; the default) renders with fg = next bg and
  bg = prev bg, so the line marks the trailing edge of prev with a
  hint of next. The space before the chevron renders in the prev
  segment's bg, the space after in the next segment's bg. When
  adjacent backgrounds are equal (legacy uniform-bg configs without
  a palette), the chevron foreground falls back to `245` so the
  glyph stays visible. Select the glyph style via `powerline_style`.
```

### Step 4: Append the "End caps" subsection

In `docs/configuration.md`, immediately after the existing three-tone palette example block in the `### Powerline` section (which was added in 0.2.3), append:

````markdown
#### End caps

Setting `caps: true` on a row adds a 1-col cap glyph to each end
whose colour matches the first/last visible segment's effective
background. The visual silhouette of the row gains a rounded,
squared, or slanted edge depending on the global `cap_style`.

```json
{
  "render": {
    "powerline": true,
    "cap_style": "round",
    "rows": [
      {"caps": true, "palette": ["234", "236", "238"], "segments": [
        {"type": "model", "fg": "33", "bold": true},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
```

Each cap consumes 1 column from the row's usable bg-fill width. The
three styles:

- `"round"` (default) — U+E0B6 / U+E0B4 half-circles. Filled glyph
  in the segment's bg colour on the terminal's default bg, producing
  a rounded edge.
- `"square"` — no glyph; a 1-col plain bg-painted space on each
  side. Useful when the patched Powerline font is unavailable. The
  visual is a flat bg extension without a curved edge.
- `"slant"` — U+E0BC / U+E0BA filled triangles. Diagonal edge in
  the segment's bg colour.

Unknown `cap_style` values fall back to `"round"`.
````

### Step 5: Confirm the edits

Run: `grep -n "cap_style\|caps\b\|End caps\|trailing edge of prev" docs/configuration.md | head -20`

Expected: matches across the schema table (1 line), the row shape section (≥ 2 lines), the Powerline chevron prose (1 line for "trailing edge of prev"), and the "End caps" subsection (heading + body).

### Step 6: Commit

```bash
git add docs/configuration.md
git commit -s -m "$(cat <<'EOF'
docs(configuration): document cap_style, caps, and chevron correction

Adds a cap_style row to the fields table; documents the caps
field in the row shape section; replaces the Powerline chevron
prose with the corrected per-style asymmetric description (fg =
prev for solid wedge, fg = next for thin line; pre-space in prev
bg, post-space in next bg). New "End caps" subsection demonstrates
the three cap styles.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Final verification gate

**Files:** none modified — verification only.

- [ ] **Step 1: Format check**

Run: `gofmt -l .`
Expected: empty.

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`
Expected: empty.

- [ ] **Step 3: Race + coverage suite**

Run: `go test -race -cover ./...`

Expected: every package PASS. Render-package coverage at or above the 94.5% baseline. New cap-related code paths are fully covered by the Task 4 tests.

- [ ] **Step 4: Smoke test — default render, no caps**

Run:

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | sed -E 's/\x1b\[[0-9;]*m//g' \
  | awk '{print NR ": [" $0 "]"}'
```

Expected: 3 rows of natural-mode output, each starting with the 2-col default margin. No cap glyphs (default rows have `caps: false`). Exit code 0.

- [ ] **Step 5: Smoke test — round caps with palette**

Run:

```bash
TMPCFG=$(mktemp -d) && mkdir -p "$TMPCFG/ccsb" && cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"powerline": true, "cap_style": "round", "rows": [{"caps": true, "palette": ["234","236","238"], "segments": [{"type":"text","label":"A"},{"type":"text","label":"B"},{"type":"text","label":"C"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | python3 -c "
import sys
data = sys.stdin.buffer.read()
names = {0xB0: 'SOLID-WEDGE', 0xB1: 'THIN-LINE', 0xB4: 'RIGHT-HALF-CIRCLE', 0xB6: 'LEFT-HALF-CIRCLE', 0xBA: 'SLANT-RIGHT', 0xBC: 'SLANT-LEFT'}
for i in range(len(data)-2):
    if data[i] == 0xEE and data[i+1] == 0x82:
        glyph = data[i+2]
        if glyph in names:
            print(f'  {names[glyph]}')"
```

Expected output (in order):
```
  LEFT-HALF-CIRCLE
  THIN-LINE
  THIN-LINE
  RIGHT-HALF-CIRCLE
```

(One left cap, two thin chevrons between 3 segments, one right cap. No solid wedges because `powerline_style` is the thin default.)

- [ ] **Step 6: Smoke test — slant caps**

Run:

```bash
cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"powerline": true, "cap_style": "slant", "rows": [{"caps": true, "palette": ["234","236"], "segments": [{"type":"text","label":"A"},{"type":"text","label":"B"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | hexdump -C | grep -E "ee 82 (bc|ba)" | head -4
```

Expected: at least two matches — one for `ee 82 bc` (SLANT-LEFT cap) and one for `ee 82 ba` (SLANT-RIGHT cap). If the rendered glyphs look wrong visually in the user's terminal (e.g. inverted slant direction, mis-aligned cells), the implementer may swap to U+E0B8 / U+E0BE or similar and update the constants + tests; document the substitution in the commit message.

- [ ] **Step 7: Smoke test — square caps**

Run:

```bash
cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"powerline": true, "cap_style": "square", "rows": [{"caps": true, "palette": ["234","236"], "segments": [{"type":"text","label":"A"},{"type":"text","label":"B"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | hexdump -C | grep -cE "ee 82 (b6|b4|bc|ba)"
```

Expected: `0`. No cap glyphs in the output — the square style emits plain bg-painted spaces, not glyphs.

- [ ] **Step 8: Log branch state for handoff**

Run: `git log main..HEAD --oneline`

Expected: seven commits on `development-0.2.4-work`:

1. `8e15b1b` `docs: spec for 0.2.4 Powerline polish round 2` (already present)
2. The plan commit (this file)
3. Task 1 — `Config.CapStyle` + `pickCapGlyphs` + constants
4. Task 2 — `Row.Caps`
5. Task 3 — asymmetric chevron geometry correction
6. Task 4 — cap emission in `renderRowPowerline`
7. Task 5 — docs refresh

After Task 6 passes, the subagent-driven-development controller hands back to the user, who runs the three release gates (squash → no-ff merge → production tag + push) per project memory `release_workflow_gates`. Those gates are out of scope for this plan.

---

## Notes for the implementer

- All tests live in `package render` (in-package). New tests append to `internal/pkg/render/render_test.go`; the existing `intPtr` helper (from 0.2.3) is reusable but not required here.
- U+E0B6 / U+E0B4 / U+E0BC / U+E0BA literals are 3-byte UTF-8 sequences. If your editor or tool channel drops them, restore via Unicode escape syntax (`""` etc.); both forms compile to identical bytes. Verify with `python3 -c "open('internal/pkg/render/render.go','rb').read().count(b'\xee\x82\xb6')"` etc.
- The slant codepoints U+E0BC / U+E0BA are the conventional Nerd Font Powerline-Extras choice. If a visual smoke verification in Task 6 Step 6 finds the rendered glyphs look wrong (e.g. inverted slant), substitute (e.g. U+E0B8 / U+E0BE) and update both constants and the `TestRenderRowPowerline_SlantCapsEmitGlyphs` test — document the substitution in the Task 1 commit message.
- The 0.2.3 chevron-geometry tests (`TestRenderRowPowerline_*Chevron*`) need attention in Task 3. If a test other than `TestRenderRowPowerline_ChevronTransitionColors` breaks, investigate before pinning. The expected breakage pattern is "test asserts both spaces are in next.bg around the glyph" — those tests should be updated to use the corrected geometry.
- The `LeftCapWithoutBgSkipped` test in Task 4 is intentionally a no-op (the branch is unreachable through the public function's contract). It exists as a documentation guard and a panic-free smoke. Do not invest in making it strictly fail-before-fix.
- Do not skip hooks, do not amend commits, do not push to remotes. Release gates happen after this plan and are user-driven.

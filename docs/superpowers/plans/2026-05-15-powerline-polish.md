# Powerline Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a default horizontal margin to every rendered row and rewrite the Powerline renderer to support alternating per-segment backgrounds with proper chevron-transition colouring (configurable thin/solid glyph).

**Architecture:** Bottom-up TDD. Pure helpers first (`effectiveMargin`, `pickGlyph`, `effectiveSegmentBg`). Then schema additions (`Config.Margin`, `Config.PowerlineStyle`, `Row.Palette`). Then `renderEnv` plumbing through `Render`, which lets `renderRowNatural` consume the margin and lets us regenerate goldens. The Powerline renderer is rewritten last because it depends on all the helpers and is the most invasive change. Documentation and a verification gate close out.

**Tech Stack:** Go, package `go.muehmer.eu/claude-cli-status-bar/internal/pkg/render`. In-package tests. No new dependencies.

**Branch:** `development-0.2.3-work` already exists with spec commit `ab0f803` as the only commit beyond `main` (`3c63448`). Plan executes on this branch.

---

## File Structure

- `internal/pkg/render/render.go` — most edits land here: schema fields (`Config.Margin`, `Config.PowerlineStyle`, `Row.Palette`), `effectiveMargin` method, `pickGlyph` function, `effectiveSegmentBg` function, `renderEnv` extension (`margin`, `powerlineStyle` fields), `Render` plumbing, `renderRowNatural` margin prepend, `renderRowPowerline` end-to-end rewrite, constant cleanup (`powerlineChevron` and `powerlineChevronFG` removed, replaced by `powerlineThinGlyph`, `powerlineSolidGlyph`, `powerlineStyleThin`, `powerlineStyleSolid`, `defaultSameBgChevronFG`, `defaultPalette`).
- `internal/pkg/render/render_test.go` — new tests appended; three existing `TestRender_*` tests updated to pin `Margin: intPtr(0)`; `intPtr` helper added at file top.
- `internal/pkg/render/testdata/golden/*.txt` — five fixtures regenerated via `-update` flag (each gains a 2-col leading margin per row).
- `docs/configuration.md` — schema-table rows for `margin` and `powerline_style`, new `palette` field in row shape section, Powerline section prose refresh, migration note, three-tone palette example.

No new files, no new packages, no new dependencies.

---

## Task 1: `Config.Margin` field + `effectiveMargin` method

**Files:**
- Modify: `internal/pkg/render/render.go` (Config struct + method + `defaultMargin` constant)
- Modify: `internal/pkg/render/render_test.go` (append tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestConfigJSON_MarginRoundTrip(t *testing.T) {
	// Nil Margin omits the key.
	out, err := json.Marshal(Config{})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(out), `"margin"`) {
		t.Errorf("nil margin must be omitted, got %s", out)
	}

	// Explicit zero emits "margin":0.
	zero := 0
	out, err = json.Marshal(Config{Margin: &zero})
	if err != nil {
		t.Fatalf("marshal zero pointer: %v", err)
	}
	if !strings.Contains(string(out), `"margin":0`) {
		t.Errorf("explicit zero must emit margin:0, got %s", out)
	}

	// Non-zero value round-trips.
	five := 5
	out, err = json.Marshal(Config{Margin: &five})
	if err != nil {
		t.Fatalf("marshal five: %v", err)
	}
	if !strings.Contains(string(out), `"margin":5`) {
		t.Errorf("Config{Margin:&5} must encode margin:5, got %s", out)
	}
	var back Config
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Margin == nil || *back.Margin != 5 {
		t.Errorf("round-trip: got %v, want pointer to 5", back.Margin)
	}
}

func TestConfig_EffectiveMargin(t *testing.T) {
	zero := 0
	five := 5
	neg := -3
	cases := []struct {
		name string
		in   *int
		want int
	}{
		{"nil defaults to 2", nil, 2},
		{"explicit zero stays zero", &zero, 0},
		{"positive passes through", &five, 5},
		{"negative clamps to zero", &neg, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Config{Margin: c.in}
			if got := cfg.effectiveMargin(); got != c.want {
				t.Errorf("effectiveMargin: got %d, want %d", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestConfigJSON_MarginRoundTrip|TestConfig_EffectiveMargin" ./internal/pkg/render -v`

Expected: compile error reporting `unknown field Margin in struct literal of type render.Config` (and `cfg.effectiveMargin undefined`).

- [ ] **Step 3: Add `Margin` field and `effectiveMargin` method**

In `internal/pkg/render/render.go`, locate the `Config` struct (currently around lines 20-33). The current shape ends with `Width int`. Add the `Margin` field plus the constant and method right after the struct.

Replace:

```go
// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
	// Width overrides automatic terminal-width detection when non-zero.
	// Powerline bg-fill and truncation use this value directly instead
	// of querying /dev/tty or the /proc parent chain. Rows are not
	// overridable via this field; discoverTermSize returns rows=0 when
	// this branch is taken, so a tty_size segment will render as
	// "<width>×0" until either /dev/tty or the /proc walk supplies a
	// row count.
	Width int `json:"width,omitempty"`
}
```

with:

```go
// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
	// Width overrides automatic terminal-width detection when non-zero.
	// Powerline bg-fill and truncation use this value directly instead
	// of querying /dev/tty or the /proc parent chain. Rows are not
	// overridable via this field; discoverTermSize returns rows=0 when
	// this branch is taken, so a tty_size segment will render as
	// "<width>×0" until either /dev/tty or the /proc walk supplies a
	// row count.
	Width int `json:"width,omitempty"`
	// Margin reserves N columns of plain (non-bg) space at the start
	// of every row and shrinks the usable bg-fill width by 2*N. The
	// goal is to leave room for Claude Code's built-in statusLine
	// chrome on each side so two-row Powerline configurations are not
	// truncated. Nil defaults to defaultMargin (= 2). Set to 0 to
	// disable; negative values clamp to 0.
	Margin *int `json:"margin,omitempty"`
}

// defaultMargin is the implicit Config.Margin when the field is unset.
const defaultMargin = 2

// effectiveMargin returns the column count to reserve as plain leading
// space and to subtract twice from the usable bg-fill width. A nil
// Margin uses defaultMargin; a negative Margin clamps to 0.
func (c Config) effectiveMargin() int {
	if c.Margin == nil {
		return defaultMargin
	}
	if *c.Margin < 0 {
		return 0
	}
	return *c.Margin
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run "TestConfigJSON_MarginRoundTrip|TestConfig_EffectiveMargin" ./internal/pkg/render -v`
Expected: 5 sub-cases PASS (1 from JSON round-trip + 4 from the table).

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all existing tests PASS. Adding a new field with `omitempty` doesn't affect any existing assertion.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Config.Margin field and effectiveMargin helper

Pointer-int with JSON omitempty so nil/missing means "use default"
(2) while explicit 0 disables. effectiveMargin clamps negatives to
0. No consumer yet — wired into renderEnv and rendering in later
tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `Config.PowerlineStyle` field + `pickGlyph` helper

**Files:**
- Modify: `internal/pkg/render/render.go` (Config struct extension + glyph constants + helper)
- Modify: `internal/pkg/render/render_test.go` (append tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestConfigJSON_PowerlineStyleRoundTrip(t *testing.T) {
	// Empty omits the key.
	out, err := json.Marshal(Config{})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(out), `"powerline_style"`) {
		t.Errorf("empty powerline_style must be omitted, got %s", out)
	}

	for _, v := range []string{"thin", "solid", "invalid"} {
		out, err := json.Marshal(Config{PowerlineStyle: v})
		if err != nil {
			t.Fatalf("marshal %q: %v", v, err)
		}
		want := `"powerline_style":"` + v + `"`
		if !strings.Contains(string(out), want) {
			t.Errorf("Config{PowerlineStyle:%q} must encode %q, got %s", v, want, out)
		}
		var back Config
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", v, err)
		}
		if back.PowerlineStyle != v {
			t.Errorf("round-trip %q: got %q", v, back.PowerlineStyle)
		}
	}
}

func TestPickGlyph(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", powerlineThinGlyph},
		{"thin", powerlineThinGlyph},
		{"solid", powerlineSolidGlyph},
		{"invalid", powerlineThinGlyph}, // unknown defaults to thin
		{"THIN", powerlineThinGlyph},    // case-sensitive, falls back to thin
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := pickGlyph(c.in); got != c.want {
				t.Errorf("pickGlyph(%q): got %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestConfigJSON_PowerlineStyleRoundTrip|TestPickGlyph" ./internal/pkg/render -v`
Expected: compile error reporting `unknown field PowerlineStyle`, `undefined: powerlineThinGlyph`, `undefined: powerlineSolidGlyph`, `undefined: pickGlyph`.

- [ ] **Step 3: Add `PowerlineStyle` field, glyph constants, and `pickGlyph`**

In `internal/pkg/render/render.go`, locate the `Config` struct (now ending with `Margin *int` from Task 1). Add `PowerlineStyle` after `Margin`:

```go
// Margin reserves N columns of plain (non-bg) space at the start
// of every row and shrinks the usable bg-fill width by 2*N. The
// goal is to leave room for Claude Code's built-in statusLine
// chrome on each side so two-row Powerline configurations are not
// truncated. Nil defaults to defaultMargin (= 2). Set to 0 to
// disable; negative values clamp to 0.
Margin *int `json:"margin,omitempty"`
// PowerlineStyle selects the chevron glyph between segments when
// Powerline is enabled. "" or "thin" (default) renders U+E0B1, the
// thin chevron. "solid" renders U+E0B0, the filled wedge. Unknown
// values silently fall back to thin so a typo cannot break
// rendering.
PowerlineStyle string `json:"powerline_style,omitempty"`
```

Locate the existing Powerline constants block (currently around lines 206-213). The 0.2.0 constants `powerlineChevron` and `powerlineChevronFG` will be removed in Task 6; for now we leave them alone and add the new glyph/style constants alongside. Replace:

```go
const (
	powerlineChevron      = ""
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2
	powerlineChevronFG      = "245"
)
```

with:

```go
const (
	powerlineChevron      = ""
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2
	powerlineChevronFG      = "245"

	// Style identifiers for Config.PowerlineStyle.
	powerlineStyleThin  = "thin"
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = ""  // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = ""  // U+E0B0 RIGHT TRIANGLE FILL

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)

// defaultPalette rotates per visible segment when neither Row.Bg
// nor Row.Palette is configured and Powerline is enabled. Three
// subtle dark greys produce a classic alternating-Powerline look
// out of the box.
var defaultPalette = []string{"234", "236", "238"}

// pickGlyph maps Config.PowerlineStyle to its chevron glyph.
// Unknown or empty values yield the thin glyph.
func pickGlyph(style string) string {
	if style == powerlineStyleSolid {
		return powerlineSolidGlyph
	}
	return powerlineThinGlyph
}
```

The literal `` and `` are U+E0B1 and U+E0B0 respectively. If transmission drops bytes, restore via Unicode escape: `""` and `""`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run "TestConfigJSON_PowerlineStyleRoundTrip|TestPickGlyph" ./internal/pkg/render -v`
Expected: all sub-cases PASS.

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all tests PASS. The new constants and helper are not yet consumed by the renderer.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Config.PowerlineStyle + pickGlyph + glyph constants

Add powerlineStyleThin/Solid string identifiers and their glyph
constants (U+E0B1, U+E0B0). pickGlyph maps the style to its
glyph; unknown values fall back to thin. defaultPalette and
defaultSameBgChevronFG are introduced for later use by the
renderer rewrite. No consumer yet — wired in the next tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `Row.Palette` schema field

**Files:**
- Modify: `internal/pkg/render/render.go` (Row struct + UnmarshalJSON pass-through)
- Modify: `internal/pkg/render/render_test.go` (append tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestRowJSON_PaletteRoundTrip(t *testing.T) {
	// Empty omits the key.
	out, err := json.Marshal(Row{Segments: []Segment{{Type: "text", Label: "x"}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), `"palette"`) {
		t.Errorf("empty palette must be omitted, got %s", out)
	}

	// Non-empty palette round-trips and coexists with bg.
	in := Row{
		Bg:       "234",
		Palette:  []string{"234", "236", "238"},
		Segments: []Segment{{Type: "text", Label: "a"}},
	}
	out, err = json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if !strings.Contains(string(out), `"palette":["234","236","238"]`) {
		t.Errorf("palette must encode as array, got %s", out)
	}
	if !strings.Contains(string(out), `"bg":"234"`) {
		t.Errorf("bg must coexist with palette, got %s", out)
	}

	var back Row
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Palette) != 3 || back.Palette[0] != "234" || back.Palette[1] != "236" || back.Palette[2] != "238" {
		t.Errorf("palette round-trip: got %v", back.Palette)
	}
	if back.Bg != "234" {
		t.Errorf("bg round-trip: got %q", back.Bg)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestRowJSON_PaletteRoundTrip ./internal/pkg/render -v`
Expected: compile error reporting `unknown field Palette in struct literal of type render.Row`.

- [ ] **Step 3: Add `Palette` field to `Row`**

In `internal/pkg/render/render.go`, locate the `Row` struct (currently around lines 39-42). Replace:

```go
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
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
}
```

The `UnmarshalJSON` for `Row` already uses the alias trick to bypass itself when decoding object form. Adding a new field to the struct is picked up automatically — no change to the unmarshaller is needed. The legacy bare-array form continues to unmarshal into `{Segments: ..., Bg: "", Palette: nil}`.

- [ ] **Step 4: Run the test**

Run: `go test -run TestRowJSON_PaletteRoundTrip ./internal/pkg/render -v`
Expected: PASS.

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all tests PASS. The new field with `omitempty` doesn't affect any existing JSON or rendering assertion. The legacy array form still works because `r.Palette = nil` is the default after the manual `r.Segments = segs; r.Bg = ""` block.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Row.Palette schema field

Per-row palette of ANSI 256-color strings. When set, rotates
across visible segments. Per-segment Segment.BG still wins for
that slot. The UnmarshalJSON dual-shape handling (legacy bare
array vs object) already routes correctly because the alias-based
object decode picks up the new field automatically.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `effectiveSegmentBg` helper

**Files:**
- Modify: `internal/pkg/render/render.go` (add helper near the other config-level helpers)
- Modify: `internal/pkg/render/render_test.go` (append tests)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestEffectiveSegmentBg(t *testing.T) {
	cases := []struct {
		name            string
		row             Row
		seg             Segment
		visibleIndex    int
		powerlineActive bool
		want            string
	}{
		{
			name: "Segment.BG overrides everything",
			row:  Row{Bg: "100", Palette: []string{"234"}},
			seg:  Segment{BG: "200"},
			want: "200",
		},
		{
			name:         "Palette rotates by visibleIndex",
			row:          Row{Palette: []string{"234", "236", "238"}},
			seg:          Segment{},
			visibleIndex: 4,
			want:         "236", // 4 % 3 == 1
		},
		{
			name: "Row.Bg used when no Palette and no Segment.BG",
			row:  Row{Bg: "100"},
			seg:  Segment{},
			want: "100",
		},
		{
			name:            "defaultPalette when Powerline and no other source",
			row:             Row{},
			seg:             Segment{},
			visibleIndex:    1,
			powerlineActive: true,
			want:            "236", // defaultPalette[1]
		},
		{
			name:            "empty string when no Powerline and no source",
			row:             Row{},
			seg:             Segment{},
			powerlineActive: false,
			want:            "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveSegmentBg(c.row, c.seg, c.visibleIndex, c.powerlineActive)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEffectiveSegmentBg_PaletteRotation(t *testing.T) {
	row := Row{Palette: []string{"234", "236", "238"}}
	want := []string{"234", "236", "238", "234", "236"}
	for i, w := range want {
		if got := effectiveSegmentBg(row, Segment{}, i, true); got != w {
			t.Errorf("visibleIndex=%d: got %q, want %q", i, got, w)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestEffectiveSegmentBg" ./internal/pkg/render -v`
Expected: compile error `undefined: effectiveSegmentBg`.

- [ ] **Step 3: Implement `effectiveSegmentBg`**

In `internal/pkg/render/render.go`, append after the `pickGlyph` function and `defaultPalette` var (which Task 2 placed near the Powerline constants block):

```go
// effectiveSegmentBg returns the background colour for a given segment
// at a given visible-segment position. Priority ladder:
//
//  1. Segment.BG (explicit per-segment override)
//  2. Row.Palette[visibleIndex % len] (per-row rotation)
//  3. Row.Bg (uniform row fill)
//  4. defaultPalette[visibleIndex % len] when powerlineActive is true
//  5. "" (no bg, natural-mode fallback)
//
// visibleIndex is the rank of the segment among the row's non-empty
// segments — empty segments do not claim a palette slot.
func effectiveSegmentBg(row Row, seg Segment, visibleIndex int, powerlineActive bool) string {
	if seg.BG != "" {
		return seg.BG
	}
	if n := len(row.Palette); n > 0 {
		return row.Palette[visibleIndex%n]
	}
	if row.Bg != "" {
		return row.Bg
	}
	if powerlineActive {
		return defaultPalette[visibleIndex%len(defaultPalette)]
	}
	return ""
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -run "TestEffectiveSegmentBg" ./internal/pkg/render -v`
Expected: 6 sub-cases PASS (5 in the table + 5 in the rotation loop, reported as one PASS per top-level test).

- [ ] **Step 5: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: all tests PASS. `effectiveSegmentBg` is not yet called by any renderer.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): effectiveSegmentBg priority ladder helper

Pure function that resolves the effective bg for a segment given
its row, its position among visible siblings, and whether
Powerline is active. Priority: Segment.BG > Row.Palette >
Row.Bg > defaultPalette (Powerline only) > "". Visible-index
rotation means dropped empty segments do not leave palette gaps.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `renderEnv` extension + `Render` plumbing + `renderRowNatural` margin + golden regeneration + existing-test fixups

This is the integration task: it wires `Config.Margin` and `Config.PowerlineStyle` into `renderEnv`, makes natural mode consume the margin, regenerates the golden fixtures (which now carry a 2-col leading margin), and pins the three existing `Render`-path tests with `Margin: intPtr(0)` so they continue to assert exact byte sequences.

**Files:**
- Modify: `internal/pkg/render/render.go` (renderEnv struct + Render env-setup + renderRowNatural)
- Modify: `internal/pkg/render/render_test.go` (add `intPtr` helper, new natural-mode test, fixups to three existing tests)
- Modify: `internal/pkg/render/testdata/golden/*.txt` (regenerate via `-update`)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
// intPtr returns a pointer to v. Used by Config tests that need to
// distinguish "explicit zero" from "unset" on Config.Margin.
func intPtr(v int) *int { return &v }

func TestRenderRowNatural_HonorsMargin(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "hello"}}}
	env := renderEnv{colorEnabled: false, margin: 3}
	got := renderRowNatural(&payload{}, row, env, " | ")
	if got != "   hello" {
		t.Errorf("got %q, want %q", got, "   hello")
	}
}

func TestRender_DefaultMarginAppliesToNaturalRows(t *testing.T) {
	cfg := Config{
		Rows: []Row{{Segments: []Segment{{Type: "text", Label: "x"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "  x" {
		t.Errorf("got %q, want %q (2 default margin spaces + 'x')", got, "  x")
	}
}

func TestRender_ExplicitZeroMarginSuppresses(t *testing.T) {
	cfg := Config{
		Margin: intPtr(0),
		Rows:   []Row{{Segments: []Segment{{Type: "text", Label: "x"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "x" {
		t.Errorf("got %q, want %q (no margin)", got, "x")
	}
}

func TestRender_MarginDegradesOnNarrowTerminal(t *testing.T) {
	// Stub /dev/tty so Render sees a deliberately narrow terminal.
	prevDev := devTTYWinsizeReader
	defer func() { devTTYWinsizeReader = prevDev }()
	devTTYWinsizeReader = func() (int, int, bool) { return 5, 24, true }

	ten := 10
	cfg := Config{
		Margin: &ten, // larger than half the terminal width
		Rows:   []Row{{Segments: []Segment{{Type: "text", Label: "x"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// 2*margin > ttyCols means margin must clamp to 0 — output has
	// no leading spaces.
	if got != "x" {
		t.Errorf("got %q, want %q (margin must clamp when terminal narrower than 2*margin)", got, "x")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run "TestRenderRowNatural_HonorsMargin|TestRender_DefaultMarginAppliesToNaturalRows|TestRender_ExplicitZeroMarginSuppresses|TestRender_MarginDegradesOnNarrowTerminal" ./internal/pkg/render -v`
Expected: compile errors first (`env.margin undefined`, then various mismatches once that's fixed).

- [ ] **Step 3: Extend `renderEnv`**

In `internal/pkg/render/render.go`, locate the `renderEnv` struct (currently around lines 321-329). Replace:

```go
type renderEnv struct {
	cwd          string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled bool   // false when NoColor was set on Options
	nowUnix      int64  // wall clock at the start of Render, for time-based segments
	ttyCols      int    // detected terminal columns, 0 when unknown
	ttyRows      int    // detected terminal rows, 0 when unknown
}
```

with:

```go
type renderEnv struct {
	cwd            string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled   bool   // false when NoColor was set on Options
	nowUnix        int64  // wall clock at the start of Render, for time-based segments
	ttyCols        int    // detected terminal columns, 0 when unknown
	ttyRows        int    // detected terminal rows, 0 when unknown
	margin         int    // plain leading spaces per row; usable bg-fill width = ttyCols - 2*margin
	powerlineStyle string // "thin" (default) | "solid"; used by renderRowPowerline via pickGlyph
}
```

- [ ] **Step 4: Plumb the new env fields in `Render`**

In `internal/pkg/render/render.go`, locate the `env := renderEnv{...}` block inside `Render` (currently around lines 276-285). Replace:

```go
	env := renderEnv{
		cwd:          cwd,
		colorEnabled: !opts.NoColor,
		nowUnix:      nowFunc().Unix(),
	}
	env.ttyCols, env.ttyRows = discoverTermSize(opts.Config)
```

with:

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

- [ ] **Step 5: Prepend margin in `renderRowNatural`**

In `internal/pkg/render/render.go`, locate `renderRowNatural` (currently around lines 301-319). Replace:

```go
func renderRowNatural(p *payload, row Row, env renderEnv, sep string) string {
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, sep)
}
```

with:

```go
func renderRowNatural(p *payload, row Row, env renderEnv, sep string) string {
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, sep)
	if env.margin > 0 {
		return strings.Repeat(" ", env.margin) + joined
	}
	return joined
}
```

- [ ] **Step 6: Run the new tests**

Run: `go test -run "TestRenderRowNatural_HonorsMargin|TestRender_DefaultMarginAppliesToNaturalRows|TestRender_ExplicitZeroMarginSuppresses|TestRender_MarginDegradesOnNarrowTerminal" ./internal/pkg/render -v`
Expected: 4 tests PASS.

- [ ] **Step 7: Identify and fix existing `Render`-path tests**

Run: `go test ./internal/pkg/render -v 2>&1 | grep -E "FAIL:" | head -20`
Expected: three test failures appear:
- `TestRender_PowerlineFalseUsesNaturalPath`
- `TestRender_PowerlineTrueEmitsBgAndChevron`
- `TestRender_PowerlineTTYColsPropagated`

Each is failing because its `Config{...}` now triggers the default margin of 2. Pin each with `Margin: intPtr(0)`:

In `internal/pkg/render/render_test.go`, find each of these test functions. Each constructs a `Config{...}` literal that flows into `Render`. Add the `Margin: intPtr(0)` field to the literal. For example, if a test reads:

```go
cfg := Config{Powerline: false, Rows: []Row{{Segments: ...}}}
```

change to:

```go
cfg := Config{Margin: intPtr(0), Powerline: false, Rows: []Row{{Segments: ...}}}
```

Apply the same change in all three named tests. Other `TestRender_*` tests that pass through `Render` and now have leading-margin output: inspect each and apply `Margin: intPtr(0)` only if its assertion compares exact bytes. Tests that use `strings.Contains` on a substring (like `TestRender_PopulatesTTYColsAndRowsViaDiscover`) need no change.

- [ ] **Step 8: Regenerate golden fixtures**

Run: `go test -run TestRender_GoldenFixtures -update ./internal/pkg/render`
Expected: PASS. The flag writes the new bytes (each row gains 2 leading spaces) to `internal/pkg/render/testdata/golden/*.txt`.

Verify the change is exactly the leading margin:

```bash
git diff internal/pkg/render/testdata/golden/ | head -40
```

Expected: each modified line of each `*.txt` file should now begin with two extra leading spaces; no other byte changes. If any other delta appears, stop and report.

- [ ] **Step 9: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: every test PASSES.

- [ ] **Step 10: Run gofmt + vet**

Run: `gofmt -l . && go vet ./...`
Expected: empty output for both.

- [ ] **Step 11: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go internal/pkg/render/testdata/golden/
git commit -s -m "$(cat <<'EOF'
feat(render): plumb Margin and PowerlineStyle through renderEnv

Render sets env.margin from Config.effectiveMargin() and
env.powerlineStyle from Config.PowerlineStyle on every invocation.
renderRowNatural prepends margin plain spaces before joining.
Golden fixtures regenerated to include the 2-col default leading
margin. Three TestRender_* tests pinned with Margin: intPtr(0) so
their exact-byte assertions stay valid against the new default.

renderRowPowerline still uses the 0.2.2 logic; its rewrite is the
next task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Rewrite `renderRowPowerline` with palette + chevron transitions + glyph selection

**Files:**
- Modify: `internal/pkg/render/render.go` (renderRowPowerline rewrite + constant cleanup)
- Modify: `internal/pkg/render/render_test.go` (append new tests for palette, transition colours, glyph selection, fallback, margin)

### Step 1: Write the failing tests

Append to `internal/pkg/render/render_test.go`:

```go
func TestRenderRowPowerline_AlternatingPaletteEmitsDistinctBgs(t *testing.T) {
	row := Row{
		Palette: []string{"234", "236", "238"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
			{Type: "text", Label: "C"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	for _, want := range []string{"\x1b[48;5;234m", "\x1b[48;5;236m", "\x1b[48;5;238m"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output\nfull: %q", want, got)
		}
	}
}

func TestRenderRowPowerline_ChevronTransitionColors(t *testing.T) {
	row := Row{
		Palette: []string{"234", "236"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	// Chevron fg = prev.bg (234), bg = next.bg (236).
	if !strings.Contains(got, "\x1b[38;5;234m") {
		t.Errorf("chevron fg = 234 missing\nfull: %q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;236m") {
		t.Errorf("chevron bg = 236 missing\nfull: %q", got)
	}
}

func TestRenderRowPowerline_ChevronUniformBgFallback(t *testing.T) {
	// Adjacent segments share the same bg via Row.Bg. The chevron fg
	// must fall back to defaultSameBgChevronFG ("245") instead of the
	// transition rule (which would make the chevron invisible).
	row := Row{
		Bg: "234",
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, "\x1b[38;5;245m") {
		t.Errorf("same-bg fallback fg = 245 missing\nfull: %q", got)
	}
}

func TestRenderRowPowerline_SolidGlyph(t *testing.T) {
	row := Row{
		Palette: []string{"234", "236"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true, powerlineStyle: powerlineStyleSolid}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, powerlineSolidGlyph) {
		t.Errorf("solid glyph missing\nfull: %q", got)
	}
	if strings.Contains(got, powerlineThinGlyph) {
		t.Errorf("thin glyph must not appear when style=solid\nfull: %q", got)
	}
}

func TestRenderRowPowerline_DefaultGlyphIsThin(t *testing.T) {
	row := Row{
		Palette:  []string{"234", "236"},
		Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
	}
	env := renderEnv{colorEnabled: true} // powerlineStyle: "" → thin
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, powerlineThinGlyph) {
		t.Errorf("thin glyph missing\nfull: %q", got)
	}
}

func TestRenderRowPowerline_DefaultPaletteUsedWhenNoConfig(t *testing.T) {
	row := Row{
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
			{Type: "text", Label: "C"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	for _, want := range []string{"\x1b[48;5;234m", "\x1b[48;5;236m", "\x1b[48;5;238m"} {
		if !strings.Contains(got, want) {
			t.Errorf("default-palette bg %q missing\nfull: %q", want, got)
		}
	}
}

func TestRenderRowPowerline_EmptySegmentDoesNotConsumePaletteSlot(t *testing.T) {
	// `mode` renders empty when neither thinking nor fast_mode is set.
	// The remaining visible segments must use palette slots 0 and 1,
	// not 0 and 2.
	row := Row{
		Palette: []string{"234", "236", "238"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "mode"}, // empty
			{Type: "text", Label: "B"},
		},
	}
	env := renderEnv{colorEnabled: true}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("first visible seg must use palette[0] = 234\nfull: %q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;236m") {
		t.Errorf("second visible seg must use palette[1] = 236\nfull: %q", got)
	}
	if strings.Contains(got, "\x1b[48;5;238m") {
		t.Errorf("palette[2] = 238 must NOT appear (only 2 visible segments)\nfull: %q", got)
	}
}

func TestRenderRowPowerline_PrependsMargin(t *testing.T) {
	row := Row{
		Palette:  []string{"234"},
		Segments: []Segment{{Type: "text", Label: "A"}},
	}
	env := renderEnv{colorEnabled: true, margin: 2}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.HasPrefix(got, "  ") {
		t.Errorf("output must start with 2 plain spaces, got %q", got)
	}
	if strings.HasPrefix(got, "  \x1b") {
		// The first 2 bytes are the margin spaces; the 3rd byte should
		// start an ANSI sequence (the bg open). Anything else is a bug.
	}
}

func TestRenderRowPowerline_UsableWidthShrunkByMargin(t *testing.T) {
	// ttyCols=80, margin=2 → usable bg-fill = 76. Three 1-col segments
	// + 2 separators of width 3 = 9 cols used. Pad = 76 - 9 = 67.
	// Plus 2 leading plain margin cols = 78 cols of visible output.
	row := Row{
		Palette: []string{"234", "236", "238"},
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
			{Type: "text", Label: "C"},
		},
	}
	env := renderEnv{colorEnabled: true, ttyCols: 80, margin: 2}
	got := renderRowPowerline(&payload{}, row, env)
	if w := displayWidth(got); w != 78 {
		t.Errorf("padded visible width: got %d, want 78 (= 2 margin + (80-4) usable)\noutput: %q", w, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run "TestRenderRowPowerline_AlternatingPalette|TestRenderRowPowerline_ChevronTransition|TestRenderRowPowerline_ChevronUniformBg|TestRenderRowPowerline_SolidGlyph|TestRenderRowPowerline_DefaultGlyph|TestRenderRowPowerline_DefaultPalette|TestRenderRowPowerline_EmptySegmentDoesNotConsume|TestRenderRowPowerline_PrependsMargin|TestRenderRowPowerline_UsableWidthShrunkByMargin" ./internal/pkg/render -v`
Expected: most fail. The 0.2.2 implementation uses uniform Row.Bg and a hardcoded chevron fg "245", so palette rotation, transition colors, glyph selection (solid case), default palette, and margin handling are all wrong. The fallback test (`ChevronUniformBgFallback`) might already pass coincidentally since the 0.2.2 chevron uses "245" — that's a happy coincidence, not a sign the rewrite is unnecessary.

- [ ] **Step 3: Rewrite `renderRowPowerline`**

In `internal/pkg/render/render.go`, locate `renderRowPowerline` (currently around lines 408-495). Replace the entire function body with:

```go
// renderRowPowerline builds one Powerline-styled row: each visible
// segment in its effective bg, joined with chevron transitions
// coloured to bleed prev.bg onto next.bg, prepended by margin plain
// spaces, padded to (ttyCols - 2*margin). When colorEnabled is
// false the function returns "" so the caller falls back to the
// natural-mode path.
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
	if !env.colorEnabled {
		return ""
	}

	// 1. Render visible segments and capture each one's effective bg.
	type renderedSeg struct {
		body string
		bg   string
	}
	var visible []renderedSeg
	for _, seg := range row.Segments {
		s := renderSegment(p, seg, env)
		if s == "" {
			continue
		}
		bg := effectiveSegmentBg(row, seg, len(visible), true)
		visible = append(visible, renderedSeg{body: s, bg: bg})
	}
	if len(visible) == 0 {
		return ""
	}

	glyph := pickGlyph(env.powerlineStyle)
	var b strings.Builder

	// 2. Leading margin: plain spaces with no bg, so Claude Code's
	//    statusLine chrome shows through.
	if env.margin > 0 {
		b.WriteString(strings.Repeat(" ", env.margin))
	}

	// 3. Walk segments, interleaving chevrons.
	for i, seg := range visible {
		if i == 0 {
			// First segment: open its bg.
			if seg.bg != "" {
				b.WriteString(bg256(seg.bg))
			}
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
		b.WriteString(seg.body)
		if seg.bg != "" {
			b.WriteString(bg256(seg.bg)) // segment body's [0m killed bg; restore
		}
	}

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

- [ ] **Step 4: Remove the now-unused 0.2.0 constants**

The constants `powerlineChevron` and `powerlineChevronFG` are no longer referenced — the new code uses `pickGlyph` for the glyph and `effectiveSegmentBg`+`defaultSameBgChevronFG` for the colour. Remove them from the constants block in `internal/pkg/render/render.go`:

```go
const (
	powerlineChevron      = ""    // REMOVE
	powerlineChevronWidth = 1
	// powerlineSeparatorWidth is the per-join layout cost in display
	// columns: one space, the chevron glyph, one space.
	powerlineSeparatorWidth = powerlineChevronWidth + 2
	powerlineChevronFG      = "245" // REMOVE

	// Style identifiers for Config.PowerlineStyle.
	powerlineStyleThin  = "thin"
	powerlineStyleSolid = "solid"

	// Glyphs used by pickGlyph based on the style identifier.
	powerlineThinGlyph  = ""  // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = ""  // U+E0B0 RIGHT TRIANGLE FILL

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)
```

becomes:

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
	powerlineThinGlyph  = ""  // U+E0B1 RIGHT TRIANGLE LINE
	powerlineSolidGlyph = ""  // U+E0B0 RIGHT TRIANGLE FILL

	// defaultSameBgChevronFG is the chevron foreground when the two
	// adjacent segments share the same effective bg (no real
	// transition). Preserves the 0.2.0 visual for legacy uniform-bg
	// configs.
	defaultSameBgChevronFG = "245"
)
```

If any existing test still references `powerlineChevron` or `powerlineChevronFG`, the deletion will surface as a compile error. The existing test `TestDisplayWidth_PowerlineChevron` uses the const for its width assertion; replace its reference to `powerlineChevron` with the literal `powerlineThinGlyph` or a hardcoded U+E0B1 literal. Similarly the existing `TestRenderRowPowerline_ChevronBetweenSegments` and `TestRenderRowPowerline_EmptySegmentDropsChevron` use `powerlineChevron` for `strings.Count` — replace with `powerlineThinGlyph`.

- [ ] **Step 5: Run the new tests**

Run: `go test -run "TestRenderRowPowerline_" ./internal/pkg/render -v`
Expected: all new powerline tests PASS plus all the existing `TestRenderRowPowerline_*` tests (after Step 4's reference updates) PASS.

- [ ] **Step 6: Run the full render-package suite**

Run: `go test ./internal/pkg/render -v`
Expected: every test PASSES.

If anything regresses, investigate. Likely candidates:
- A test referencing one of the removed constants → fix to use the renamed/new constants.
- A test asserting exact byte sequences that span the removed `\x1b[39m`-vs-`\x1b[0m` chevron close → update the expected bytes to match the new full-reset pattern. The new code emits `\x1b[0m` (full reset) after the glyph rather than the 0.2.0 `\x1b[39m` (fg-only). Tests that pin this exact difference must be updated; tests that use substring assertions are unaffected.

- [ ] **Step 7: Run gofmt + vet**

Run: `gofmt -l . && go vet ./...`
Expected: empty output for both.

- [ ] **Step 8: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): Powerline rewrite — palette rotation + chevron transitions

renderRowPowerline now captures the effective bg of each visible
segment via effectiveSegmentBg, prepends env.margin plain spaces,
emits glyph-aware chevrons (pickGlyph picks thin or solid) whose
fg is the prev.bg and bg is the next.bg (with the
defaultSameBgChevronFG "245" fallback when adjacent bgs match),
and pads to (ttyCols - 2*margin).

The 0.2.0 constants powerlineChevron and powerlineChevronFG are
removed — the glyph is now picked dynamically and the chevron
foreground is computed from adjacent bgs.

Tests cover alternating palette emission, transition colours, the
same-bg fallback, glyph selection, default-palette fallback,
empty-segment-no-slot, margin prepending, and the usable-width
math under margin.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Documentation refresh

**Files:**
- Modify: `docs/configuration.md` (schema-table rows + row shape extension + Powerline section refresh + migration note + new example)

### Step 1: Add `margin` and `powerline_style` to the schema table

In `docs/configuration.md`, locate the `### Fields` table (currently around lines 39-44). Append two new rows so the table ends with:

```
| `powerline` | bool            | `false`       | When true, each row's `bg` fills the terminal width and segments are joined with the U+E0B1 thin chevron. See [Powerline](#powerline). |
| `width`     | int             | `0`           | Explicit terminal-cols override. When `> 0`, ccsb skips terminal-size detection and uses this value. Default `0` runs the detection chain (`/dev/tty` ioctl, then `/proc` parent-process walk). |
| `margin`    | int             | `2`           | Plain (no-bg) leading spaces per row; usable bg-fill width is shrunk by `2*margin`. Leaves room for Claude Code's built-in statusLine chrome on each side. Set to `0` to disable. Defaults to `2` when omitted; negative values clamp to `0`. |
| `powerline_style` | string    | `"thin"`      | Chevron glyph in Powerline mode. `"thin"` (default) renders U+E0B1; `"solid"` renders U+E0B0 as a filled wedge. Unknown values silently fall back to `"thin"`. |
```

### Step 2: Add the `palette` field to the row shape section

In `docs/configuration.md`, locate the `### Row shape` section (currently around lines 59-79). After the existing description of the object form, append:

````markdown
Within the object form, an optional `palette` field accepts an array
of ANSI 256-color strings:

```json
{"palette": ["234", "236", "238"], "segments": [...]}
```

When set, the palette rotates across the row's **visible** segments —
empty segments (e.g. `mode` when neither thinking nor fast_mode is
set) do not consume a palette slot. Index N takes
`palette[N % len(palette)]`.

The effective background of each segment is resolved in priority
order: explicit `Segment.bg` > `Row.palette` rotation > `Row.bg`
(uniform fill) > built-in `defaultPalette` of three subtle dark
greys (`["234", "236", "238"]`, applied only when Powerline is
enabled). If all four are unset and Powerline is off, the segment
has no background.
````

### Step 3: Refresh the Powerline section's chevron prose

In `docs/configuration.md`, locate the bullet about the chevron in the `### Powerline` section (currently around lines 98-102, the line that mentions "muted-grey foreground (`245`)"). Replace:

```
- Segments are joined with the U+E0B1 thin chevron in a muted-grey
  foreground (`245`), with a single space on each side for breathing
  room. The chevron has no background of its own, so the row's
  background shows through the spaces and the glyph.
```

with:

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

Also locate the line near the end of the `### Powerline` section that reads:

```
The chevron glyph and its foreground are hardcoded in 0.2.0; future
releases may expose them as config knobs.
```

Replace with:

```
The chevron glyph is selectable via `powerline_style`; its colours
are derived from adjacent backgrounds.
```

### Step 4: Add a three-tone palette example

Inside the `### Powerline` section, immediately after the existing two-tone palette example block (the one with `{"bg": "234", "segments": [...]}` and `{"bg": "237", "segments": [...]}`), append:

````markdown
For a classic alternating-Powerline look within a single row, use
`palette` instead of a single `bg`:

```json
{
  "render": {
    "powerline": true,
    "rows": [
      {"palette": ["234", "236", "238"], "segments": [
        {"type": "model", "fg": "33", "bold": true},
        {"type": "context", "style": "bar+pct", "fg": "245"},
        {"type": "limit_5h", "fg": "245"},
        {"type": "limit_7d", "fg": "245"}
      ]},
      {"palette": ["237", "238", "237"], "segments": [
        {"type": "git_branch", "fg": "33"},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
```

Each visible segment gets a bg from its row's palette (modulo
rotation), and the chevron between segments transitions between
those bgs. When `powerline` is `true` and neither `bg` nor
`palette` is set on a row, the built-in default palette
`["234", "236", "238"]` is applied automatically.
````

### Step 5: Add a migration note

Below the new three-tone example block, append:

````markdown
**Migration from uniform `bg` to `palette`** (0.2.2 → 0.2.3): if
your existing config uses a single `bg` per row, the visual is
preserved unchanged in 0.2.3 — chevrons fall back to the muted-grey
`245` foreground on the uniform background. To opt into the
alternating-Powerline look, replace `bg` with `palette` on each
row:

```diff
-{"bg": "234", "segments": [...]}
+{"palette": ["234", "236", "238"], "segments": [...]}
```

You can keep `bg` alongside `palette` — `palette` wins for segment
fills, and `bg` becomes the row-level fallback that the renderer
ignores when the palette is non-empty.
````

### Step 6: Confirm the edits

Run: `grep -n "margin\|powerline_style\|palette\|defaultPalette\|migration" docs/configuration.md | head -20`
Expected: multiple matches across the schema table, row shape section, Powerline section, and migration note.

### Step 7: Commit

```bash
git add docs/configuration.md
git commit -s -m "$(cat <<'EOF'
docs(configuration): document margin, powerline_style, palette

Two new schema fields (margin, powerline_style) in the fields
table; new palette array in the row shape section; Powerline
chevron prose updated to describe transition colouring and the
same-bg fallback; three-tone palette example; migration note from
uniform bg to palette.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Final verification gate

**Files:** none modified — verification only.

- [ ] **Step 1: Format check**

Run: `gofmt -l .`
Expected: empty.

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`
Expected: empty.

- [ ] **Step 3: Race + coverage suite**

Run: `go test -race -cover ./...`
Expected: every package PASS. Render-package coverage at or above the 94.5% baseline.

- [ ] **Step 4: Smoke test — default natural-mode render with default margin**

Run:

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"session_id":"x","model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | sed -E 's/\x1b\[[0-9;]*m//g' \
  | awk '{print NR ": [" $0 "]"}'
```

Expected: 3 rows of natural-mode output, each starting with 2 leading spaces from the default margin. The third row prints `[  tmp]` (cwd basename) — the brackets in the awk wrapper make trailing whitespace explicit. No errors on stderr. Exit code 0.

- [ ] **Step 5: Smoke test — three-tone palette in Powerline mode**

Run:

```bash
TMPCFG=$(mktemp -d) && mkdir -p "$TMPCFG/ccsb" && cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"powerline": true, "rows": [{"palette": ["234","236","238"], "segments": [{"type":"text","label":"A"},{"type":"text","label":"B"},{"type":"text","label":"C"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A | head -3
```

Expected: a single row whose raw ANSI sequences contain `\x1b[48;5;234m`, `\x1b[48;5;236m`, `\x1b[48;5;238m` (one per segment) and two chevron blocks each with `\x1b[38;5;234m` / `\x1b[38;5;236m` as the transition fgs. The row starts with two plain spaces (the default margin).

- [ ] **Step 6: Smoke test — `solid` glyph**

Run:

```bash
cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"powerline": true, "powerline_style": "solid", "rows": [{"palette": ["234","236"], "segments": [{"type":"text","label":"A"},{"type":"text","label":"B"}]}]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A | head -3
```

Expected: the output contains the U+E0B0 byte sequence (raw bytes `\xee\x82\xb0`) and does NOT contain U+E0B1 (`\xee\x82\xb1`).

- [ ] **Step 7: Smoke test — `margin: 0` flush rendering**

Run:

```bash
cat > "$TMPCFG/ccsb/config.json" <<'EOF'
{"render": {"margin": 0, "rows": [[{"type":"text","label":"flush"}]]}}
EOF
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb
```

Expected: output is the literal string `flush\n` (no leading whitespace), confirming explicit zero disables the default margin.

- [ ] **Step 8: Log branch state for handoff**

Run: `git log main..HEAD --oneline`

Expected: nine commits on `development-0.2.3-work`:

1. `ab0f803` `docs: spec for 0.2.3 Powerline polish` (already present)
2. The plan commit (this file)
3. Task 1 — `Config.Margin` + `effectiveMargin`
4. Task 2 — `Config.PowerlineStyle` + `pickGlyph` + glyph constants
5. Task 3 — `Row.Palette` schema
6. Task 4 — `effectiveSegmentBg`
7. Task 5 — `renderEnv` + plumbing + `renderRowNatural` margin + goldens
8. Task 6 — `renderRowPowerline` rewrite
9. Task 7 — docs

After Task 8 passes, the subagent-driven-development controller hands back to the user, who runs the three release gates (squash → no-ff merge → production tag + push) per project memory `release_workflow_gates`. Those gates are out of scope for this plan.

---

## Notes for the implementer

- All tests live in `package render` (in-package). New tests append to the existing `*_test.go` files; never create a new test file.
- The U+E0B1 (`powerlineThinGlyph`) and U+E0B0 (`powerlineSolidGlyph`) literals are written as the bare glyphs in source. If your editor or tool channel drops bytes during transmission, use `""` and `""` instead — both compile to identical bytes.
- The `×` Unicode multiplication sign in `renderTTYSize` (from 0.2.2) is unchanged.
- `intPtr(v int) *int` lives in `render_test.go`. Reuse it for every `Config{Margin: intPtr(N)}` test literal.
- The golden test runner takes the `-update` flag (`go test -run TestRender_GoldenFixtures -update`). After Task 5 regenerates the fixtures, inspect the diff to confirm only leading-margin bytes changed.
- The existing tests that fail after the new default margin lands are the **three** `TestRender_*` tests named in the spec. If a fourth test fails after the margin lands, investigate before pinning — it might indicate a real regression rather than a default-shift.
- The 0.2.2 chevron close used `\x1b[39m` (fg-only) to preserve bg. The 0.2.3 chevron close uses `\x1b[0m` (full reset) because bg changes at the transition. Tests that pinned `\x1b[39m` will need their expected bytes updated.
- Do not skip hooks, do not amend commits, do not push to remotes. Release gates happen after this plan and are user-driven.

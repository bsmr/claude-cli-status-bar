# Dynamic `Shrink` segment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-segment `Shrink` flag so that, on a narrow terminal, a designated segment (`cwd` in the default config) is dynamically truncated with `…` just enough to keep the rest of the row — notably the right-aligned `version` — rendered in full.

**Architecture:** A new boolean field `Segment.Shrink`. A pre-pass `applyShrink`, run per output row in `Render` (after `expandWrappedRows`, before the row-mode dispatch), measures the row exactly as `rowOverflows` does; on overflow it lowers the *effective* `MaxWidth` of each `Shrink`-marked segment by the overflow deficit (floored at 1 column). The actual cut is delegated to the existing `renderSegment` → `truncateToWidth` path, so no new ANSI-aware truncation code is added.

**Tech Stack:** Go, `github.com/mattn/go-runewidth` (already a dependency), standard `testing`.

**Spec:** `docs/superpowers/specs/2026-06-03-shrink-segment-design.md`

**Scope note:** This plan covers code + godoc + unit/integration tests only. The user-facing `docs/configuration.md` / README documentation of the `shrink` field is deliberately left to a separate docs patch, per the project's "doc-refresh is its own version" rule.

---

## File Structure

- **Modify** `internal/pkg/render/render.go`
  - Add `Shrink bool` field to the `Segment` struct (after `MinCols`).
  - Add `shrinkFloor` const, `hasAnyShrink` helper, and `applyShrink` function (next to `hasAnyWrap` / `rowOverflows`).
  - Add `Shrink: true` to the `cwd` segment in `defaultConfig` row 2.
  - Wire `applyShrink` into the per-row loop in `Render`.
- **Modify** `internal/pkg/render/render_test.go`
  - JSON round-trip test for the new field.
  - Unit tests for `hasAnyShrink` and `applyShrink`.
  - Integration tests through `Render` (custom config + default config).

---

## Task 1: Add the `Segment.Shrink` field

**Files:**
- Modify: `internal/pkg/render/render.go` (Segment struct, after the `MinCols` field ~line 259)
- Test: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/render/render_test.go`:

```go
// --- 0.2.33 dynamic shrink ---------------------------------------------------

func TestSegmentShrink_JSONRoundTrip(t *testing.T) {
	in := Segment{Type: "cwd", Shrink: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"shrink":true`) {
		t.Errorf("Shrink must encode as \"shrink\":true, got %s", b)
	}
	var out Segment
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Shrink {
		t.Errorf("Shrink must survive round-trip, got %+v", out)
	}
	// Zero value must be omitted so existing configs stay byte-stable.
	z, _ := json.Marshal(Segment{Type: "cwd"})
	if strings.Contains(string(z), "shrink") {
		t.Errorf("zero Shrink must be omitted, got %s", z)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pkg/render/ -run TestSegmentShrink_JSONRoundTrip -v`
Expected: FAIL — compile error `unknown field 'Shrink' in struct literal of type Segment`.

- [ ] **Step 3: Add the field**

In `internal/pkg/render/render.go`, inside the `Segment` struct, immediately after the `MinCols int` field (and before the closing `}` of the struct ~line 259-260), add:

```go

	// Shrink, when true, marks this segment as the one that yields
	// display width when its row would overflow. After reflow, a
	// pre-pass (applyShrink) measures the row; if the visible content
	// exceeds the usable width (ttyCols - 2*margin, minus cap columns
	// when Caps is active), every Shrink-marked segment has its
	// effective MaxWidth lowered — in row order, each yielding down to
	// a 1-column floor (the "…" glyph) — until the deficit is covered.
	// The truncation itself reuses the MaxWidth path (renderSegment →
	// truncateToWidth), so it is safe only on text-style segments
	// (cwd, git_branch, text, model, …) for the same reason MaxWidth is.
	// A user-supplied MaxWidth is only ever lowered further, never
	// raised. When ttyCols is unknown (0) or the row already fits, the
	// flag is a no-op. Lets the right-aligned version stamp stay intact
	// on narrow terminals while cwd shortens to absorb the overflow.
	Shrink bool `json:"shrink,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/render/ -run TestSegmentShrink_JSONRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -m "feat: add Segment.Shrink field

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `hasAnyShrink` helper + `applyShrink` pre-pass

**Files:**
- Modify: `internal/pkg/render/render.go` (add near `hasAnyWrap` ~line 757 and `rowOverflows` ~line 777)
- Test: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestHasAnyShrink(t *testing.T) {
	none := []Segment{{Type: "text"}, {Type: "cwd"}}
	if hasAnyShrink(none) {
		t.Error("no Shrink segment must report false")
	}
	some := []Segment{{Type: "text"}, {Type: "cwd", Shrink: true}}
	if !hasAnyShrink(some) {
		t.Error("a Shrink segment must report true")
	}
}

func TestApplyShrink_NoopWhenTtyColsUnknown(t *testing.T) {
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAAAAAAAA", Shrink: true},
		{Type: "text", Label: "BBBB"},
	}}
	out := applyShrink(&payload{}, row, renderEnv{ttyCols: 0}, false, " | ")
	if out[0].MaxWidth != 0 {
		t.Errorf("ttyCols=0 must not set MaxWidth, got %d", out[0].MaxWidth)
	}
}

func TestApplyShrink_NoopWhenNoShrinkSegment(t *testing.T) {
	// Overflowing row, but nothing is marked Shrink.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA"},
		{Type: "text", Label: "BBBBBBBBBB"},
	}}
	env := renderEnv{ttyCols: 8, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	for i, s := range out {
		if s.MaxWidth != 0 {
			t.Errorf("segment %d MaxWidth must stay 0, got %d", i, s.MaxWidth)
		}
	}
}

func TestApplyShrink_NoopWhenRowFits(t *testing.T) {
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAA", Shrink: true},
		{Type: "text", Label: "BBBB"},
	}}
	env := renderEnv{ttyCols: 80, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 0 {
		t.Errorf("fitting row must not set MaxWidth, got %d", out[0].MaxWidth)
	}
}

func TestApplyShrink_LowersMaxWidthToFitExactly(t *testing.T) {
	// Widths: shrink body 10, fixed body 6, one " | " separator (3).
	// used = 10 + 6 + 3 = 19. usable = 14. deficit = 5.
	// shrink body must drop to 10 - 5 = 5.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA", Shrink: true}, // 10
		{Type: "text", Label: "BBBBBB"},                   // 6
	}}
	env := renderEnv{ttyCols: 14, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 5 {
		t.Errorf("shrink MaxWidth = %d, want 5", out[0].MaxWidth)
	}
	if out[1].MaxWidth != 0 {
		t.Errorf("non-shrink segment must be untouched, got %d", out[1].MaxWidth)
	}
}

func TestApplyShrink_FloorsAtOneColumn(t *testing.T) {
	// usable far smaller than the fixed segment alone: shrink can only
	// reach the 1-col floor; the row still overflows (best-effort).
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA", Shrink: true}, // 10
		{Type: "text", Label: "BBBBBBBBBB"},               // 10
	}}
	env := renderEnv{ttyCols: 4, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 1 {
		t.Errorf("shrink MaxWidth must floor at 1, got %d", out[0].MaxWidth)
	}
}

func TestApplyShrink_NeverRaisesExistingMaxWidth(t *testing.T) {
	// Fits comfortably; an already-capped shrink segment is left alone.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA", Shrink: true, MaxWidth: 4}, // renders width 4
		{Type: "text", Label: "BB"},
	}}
	env := renderEnv{ttyCols: 80, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 4 {
		t.Errorf("existing MaxWidth must be preserved when row fits, got %d", out[0].MaxWidth)
	}
}

func TestApplyShrink_DistributesDeficitInRowOrder(t *testing.T) {
	// Two shrink segments (10 + 10) + fixed (10) + 2 separators (6) = 36.
	// usable = 30 → deficit 6. First shrink yields all 6 (10→4); second
	// stays full.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA", Shrink: true}, // 10
		{Type: "text", Label: "CCCCCCCCCC", Shrink: true}, // 10
		{Type: "text", Label: "BBBBBBBBBB"},               // 10
	}}
	env := renderEnv{ttyCols: 30, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 4 {
		t.Errorf("first shrink MaxWidth = %d, want 4", out[0].MaxWidth)
	}
	if out[1].MaxWidth != 0 {
		t.Errorf("second shrink must stay untouched (deficit already covered), got %d", out[1].MaxWidth)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/render/ -run 'TestHasAnyShrink|TestApplyShrink' -v`
Expected: FAIL — compile error `undefined: hasAnyShrink` / `undefined: applyShrink`.

- [ ] **Step 3: Implement the helper, const, and function**

In `internal/pkg/render/render.go`, directly after the `hasAnyWrap` function (~line 764), add:

```go

// shrinkFloor is the minimum visible width an applyShrink candidate is
// reduced to — one column, the width of the "…" ellipsis glyph.
const shrinkFloor = 1

// hasAnyShrink reports whether any segment carries the Shrink flag.
// Cheap pre-check so applyShrink can skip the per-segment render loop
// on rows with nothing to shrink.
func hasAnyShrink(segs []Segment) bool {
	for _, s := range segs {
		if s.Shrink {
			return true
		}
	}
	return false
}

// applyShrink returns row.Segments unchanged, or — when the row's
// visible content would overflow the usable width AND the row carries
// at least one Shrink-marked segment — a copy in which each Shrink
// segment's effective MaxWidth is lowered (in row order, flooring at
// shrinkFloor) until the overflow deficit is absorbed. The actual
// truncation happens later in renderSegment → truncateToWidth; this
// function only computes and stamps the cap.
//
// Measurement mirrors rowOverflows exactly (segment body widths via
// displayWidth, which strips ANSI; per-style separator cost; cap
// columns when powerlineActive && Caps), so the gap-free width here
// matches the real overflow condition — when used > usable a
// right-aligned segment's padding gap is already zero.
//
// When ttyCols is unknown (0), no Shrink segment exists, only one
// segment is visible, or the row already fits, the input slice is
// returned untouched. A pre-existing MaxWidth is only ever lowered,
// never raised, because the measured width already reflects it and the
// new cap is strictly smaller.
func applyShrink(p *payload, row Row, env renderEnv, powerlineActive bool, sep string) []Segment {
	if env.ttyCols == 0 || !hasAnyShrink(row.Segments) {
		return row.Segments
	}
	type cand struct {
		idx   int
		width int
	}
	var cands []cand
	used := 0
	visible := 0
	for i, seg := range row.Segments {
		body := renderSegment(p, seg, env)
		if body == "" {
			continue
		}
		w := displayWidth(body)
		used += w
		visible++
		if seg.Shrink {
			cands = append(cands, cand{idx: i, width: w})
		}
	}
	if len(cands) == 0 || visible <= 1 {
		return row.Segments
	}
	if powerlineActive {
		used += (visible - 1) * powerlineSeparatorWidth
	} else {
		used += (visible - 1) * displayWidth(sep)
	}
	usable := env.ttyCols - 2*env.margin
	if powerlineActive && row.Caps {
		usable -= 2
	}
	deficit := used - usable
	if deficit <= 0 {
		return row.Segments
	}
	out := make([]Segment, len(row.Segments))
	copy(out, row.Segments)
	for _, c := range cands {
		if deficit <= 0 {
			break
		}
		canGive := c.width - shrinkFloor
		if canGive <= 0 {
			continue
		}
		take := min(canGive, deficit)
		out[c.idx].MaxWidth = c.width - take
		deficit -= take
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/render/ -run 'TestHasAnyShrink|TestApplyShrink' -v`
Expected: PASS (all 9 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -m "feat: add applyShrink pre-pass and hasAnyShrink helper

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Wire `applyShrink` into `Render` and mark default `cwd` as Shrink

**Files:**
- Modify: `internal/pkg/render/render.go` (per-row loop in `Render` ~line 936; `defaultConfig` cwd segment ~line 631)
- Test: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_ShrinkKeepsRightAlignedVersionIntact_CustomConfig(t *testing.T) {
	// Long branch stand-in + long cwd + right-aligned version on a
	// narrow tty. cwd carries Shrink, so it must truncate (…) while the
	// version string survives in full and the row stays within width.
	raw := []byte(`{"workspace":{"current_dir":"/home/u/very/long/path/to/some-project-dir"}}`)
	cfg := Config{
		Width:  40,
		Margin: new(0),
		Rows: []Row{{
			Segments: []Segment{
				{Type: "text", Label: "feature/some-long-branch-name"},
				{Type: "cwd", Format: "full", Shrink: true},
				{Type: "text", Label: "v9.9.9", Align: "right"},
			},
		}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	line := strings.Split(got, "\n")[0]
	if !strings.Contains(line, "v9.9.9") {
		t.Errorf("version must survive in full, got %q", line)
	}
	if !strings.Contains(line, "…") {
		t.Errorf("cwd should have been truncated with an ellipsis, got %q", line)
	}
	if w := displayWidth(line); w > 40 {
		t.Errorf("row width = %d, must not exceed usable 40: %q", w, line)
	}
}

func TestRender_ShrinkDefaultConfigProtectsVersion_Powerline(t *testing.T) {
	// Default config (empty Rows): row 2 is git_branch | lines | cwd |
	// version(right). With no git dir / lines, only cwd + version show.
	// A long cwd on a narrow tty must shrink so the version is intact.
	raw := []byte(`{"workspace":{"current_dir":"/home/u/very/long/path/to/some-project-directory-name"}}`)
	got, err := Render(Options{
		Config:  Config{Width: 40},
		Version: "9.9.9",
	}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(got, "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "v9.9.9") {
		t.Errorf("default-config version must survive in full, got %q", last)
	}
	if !strings.Contains(last, "…") {
		t.Errorf("default cwd should shrink with an ellipsis, got %q", last)
	}
	if w := displayWidth(last); w > 40 {
		t.Errorf("last row width = %d, must not exceed usable 40: %q", w, last)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkg/render/ -run TestRender_Shrink -v`
Expected: FAIL — the version string is clipped / the row exceeds width because `applyShrink` is not yet wired in (and the default `cwd` lacks `Shrink`).

- [ ] **Step 3a: Mark the default `cwd` segment as Shrink**

In `internal/pkg/render/render.go`, in `defaultConfig` row 2 (~line 631), change:

```go
				{Type: "cwd", FG: "245"},
```

to:

```go
				{Type: "cwd", FG: "245", Shrink: true},
```

- [ ] **Step 3b: Wire `applyShrink` into the per-row loop**

In `internal/pkg/render/render.go`, in `Render`, inside the `for outputIdx, row := range rows` loop (~line 936-947), add the shrink pre-pass right after `rowEnv.paletteStart` is set and before the `switch`:

```go
	for outputIdx, row := range rows {
		rowEnv := env
		rowEnv.paletteStart = outputIdx * stride
		row.Segments = applyShrink(&p, row, rowEnv, powerlineActive, sep)
		var line string
		switch {
		case row.Align == "right":
			line = renderRowRight(&p, row, rowEnv, sep)
		case cfg.Powerline && env.colorEnabled:
			line = renderRowPowerline(&p, row, rowEnv)
		default:
			line = renderRowNatural(&p, row, rowEnv, sep)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
```

(`powerlineActive` is already declared above the loop as `cfg.Powerline && env.colorEnabled`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkg/render/ -run TestRender_Shrink -v`
Expected: PASS (both integration tests).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -m "feat: wire applyShrink into Render; default cwd shrinks

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full render suite with race + coverage**

Run: `go test -race -cover ./internal/pkg/render/`
Expected: PASS, no data races. Note coverage %.

- [ ] **Step 2: Run the whole module test suite**

Run: `go test ./...`
Expected: all packages PASS. The golden-fixture tests (pinned at `Config.Width: 200`) must stay green — at width 200 nothing overflows, so `applyShrink` is a no-op and existing goldens are unaffected.

- [ ] **Step 3: Static checks and formatting**

Run: `go vet ./... && gofmt -l .`
Expected: `go vet` clean; `gofmt -l .` prints nothing.

- [ ] **Step 4: Build and smoke-test the real binary at a narrow width**

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"workspace":{"current_dir":"/home/u/very/long/path/to/some-project-directory-name"}}' \
  | ./bin/ccsb
```

Expected: the binary renders without error. (Width auto-detection applies here; the deterministic narrow-width behaviour is already proven by the integration tests in Task 3. Visually confirm the version stamp is present when the terminal is narrow.)

- [ ] **Step 5: Final commit if anything was reformatted**

```bash
gofmt -w .
git diff --quiet || git commit -am "chore: gofmt

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review checklist (completed by plan author)

- **Spec coverage:** `Segment.Shrink` field (Task 1) ✓; `applyShrink` with floor, deficit math, MaxWidth-never-raised, multi-segment row-order yield (Task 2) ✓; placement after `expandWrappedRows` / before dispatch + default `cwd` Shrink (Task 3) ✓; right-align interaction validated by integration tests (Task 3) ✓; best-effort fallback validated by `TestApplyShrink_FloorsAtOneColumn` (Task 2) ✓; reuse of `truncateToWidth` `…` (Task 2 implementation delegates to `renderSegment`) ✓. Non-goals (no `git_branch` default shrink, no weighting) respected.
- **Placeholder scan:** none — every step shows full code or an exact command.
- **Type consistency:** `applyShrink(*payload, Row, renderEnv, bool, string) []Segment` and `hasAnyShrink([]Segment) bool` and `shrinkFloor` used identically in tests and implementation; `min` is the Go builtin (already used elsewhere in this file via `max`).

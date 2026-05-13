# Powerline renderer implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Powerline renderer for ccsb 0.2.0 — top-level `Config.Powerline` toggle, per-row background fill that extends to the terminal's right edge, thin chevron separators between segments, plus a universal space-after-colon format fix across all label-bearing segments.

**Architecture:** A new `renderRowPowerline` path runs only when `Config.Powerline` is true and color is enabled; otherwise the existing row-join path (now extracted as `renderRowNatural`) handles rendering. The schema upgrades `Rows` from `[][]Segment` to `[]Row` where `Row` is a struct with `Segments` and an optional `Bg`. Backward compatibility is preserved at the JSON layer via a custom `Row.UnmarshalJSON` that accepts both the old array shape and the new object shape; the Go API changes are part of the minor-version bump.

**Tech Stack:** Go 1.26, plus two new dependencies introduced as they are needed: `golang.org/x/sys/unix` (for the `TIOCGWINSZ` ioctl in Task 1) and `github.com/mattn/go-runewidth` (for emoji/CJK-aware width measurement in Task 2).

**Reference spec:** `docs/superpowers/specs/2026-05-13-powerline-design.md`.

## File structure

- Create: `internal/pkg/render/tty.go` — `readTTYCols` + the package-private `ttyColsFunc` indirection for tests.
- Create: `internal/pkg/render/tty_test.go` — fake-`ttyColsFunc` tests.
- Modify: `internal/pkg/render/render.go` — `displayWidth` helper, `Row` struct + `UnmarshalJSON`, `Config.Powerline`, `renderEnv.ttyCols`, `renderRowPowerline`, dispatch tweak in `Render`, `renderRowNatural` extraction.
- Modify: `internal/pkg/render/segments.go` — universal space-after-colon in `renderEffort`, `renderOutputStyle`, `renderLimit`, `renderGitBranch`.
- Modify: `internal/pkg/render/render_test.go` — schema-upgrade rewrites (existing tests' `[][]Segment` → `[]Row{...}`), new Powerline tests, new spacing tests.
- Modify: `internal/pkg/render/testdata/golden/*.txt` — five fixtures regenerated for the new spacing.
- Modify: `docs/configuration.md` — Powerline section, Row shape, spacing, NO_COLOR fallback, worked example.
- Modify: `go.mod`, `go.sum` — two `go get` invocations.

No new files outside `internal/pkg/render/`. Existing test patterns (in-package tests with direct access to `payload`, `Segment`, etc.) are preserved.

---

## Task 1: TTY-cols helper

**Files:**
- Create: `internal/pkg/render/tty.go`
- Create: `internal/pkg/render/tty_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the `golang.org/x/sys` dependency**

Run: `go get golang.org/x/sys/unix`

Expected: `go.mod` and `go.sum` updated; no other diff. Verify with `git diff -- go.mod`.

- [ ] **Step 2: Write the failing tests**

Create `internal/pkg/render/tty_test.go`:

```go
package render

import "testing"

func TestReadTTYColsFunc_DefaultReturnsZeroOrPositive(t *testing.T) {
	// The real readTTYCols opens /dev/tty. In a `go test` run there may
	// be no controlling tty, in which case 0 is returned. We assert
	// only that the function does not panic and returns a non-negative
	// value.
	got := readTTYCols()
	if got < 0 {
		t.Errorf("readTTYCols(): got %d, want >= 0", got)
	}
}

func TestTTYColsFunc_IsIndirectedForTests(t *testing.T) {
	// ttyColsFunc is a package-level var that tests can swap. Swapping
	// must take effect for the next call.
	prev := ttyColsFunc
	defer func() { ttyColsFunc = prev }()

	ttyColsFunc = func() int { return 128 }
	if got := ttyColsFunc(); got != 128 {
		t.Errorf("ttyColsFunc() with fake: got %d, want 128", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestReadTTYColsFunc|TestTTYColsFunc' -v -count=1`

Expected: compile error — `undefined: readTTYCols` and `undefined: ttyColsFunc`.

- [ ] **Step 4: Implement the TTY helper**

Create `internal/pkg/render/tty.go`:

```go
package render

import (
	"os"

	"golang.org/x/sys/unix"
)

// readTTYCols returns the number of columns of the controlling
// terminal, or 0 if the size cannot be determined.
//
// ccsb's stdin is the JSON payload pipe and its stdout is the pipe to
// Claude Code, so neither carries terminal dimensions. /dev/tty is the
// controlling terminal of the spawned process, which Claude Code
// inherits from its own controlling tty. If the process has no
// controlling tty or the ioctl fails, this returns 0 and the
// Powerline renderer falls back to natural width.
func readTTYCols() int {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return 0
	}
	defer f.Close()
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0
	}
	return int(ws.Col)
}

// ttyColsFunc is the indirection point so tests can swap in a
// deterministic fake. Production code calls ttyColsFunc, never
// readTTYCols directly.
var ttyColsFunc = readTTYCols
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestReadTTYColsFunc|TestTTYColsFunc' -v -count=1`

Expected: both tests PASS. Then full suite:

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/pkg/render/tty.go internal/pkg/render/tty_test.go
git commit -s -m "feat(render): readTTYCols helper plus ttyColsFunc indirection

Opens /dev/tty and queries TIOCGWINSZ. Returns 0 on any error so
the Powerline renderer can fall back to natural width when no
controlling tty is available. Tests swap the package-level
ttyColsFunc to a fake; the real readTTYCols is exercised by a
non-negative-return sanity check that tolerates 'no controlling
tty' in the go test environment.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `displayWidth` helper

**Files:**
- Modify: `internal/pkg/render/render.go`
- Modify: `internal/pkg/render/render_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the `go-runewidth` dependency**

Run: `go get github.com/mattn/go-runewidth`

Expected: `go.mod` and `go.sum` updated.

- [ ] **Step 2: Write the failing tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestDisplayWidth_PlainASCII(t *testing.T) {
	if got := displayWidth("hello"); got != 5 {
		t.Errorf("displayWidth(\"hello\"): got %d, want 5", got)
	}
}

func TestDisplayWidth_StripsANSI(t *testing.T) {
	in := "\x1b[1m\x1b[38;5;33mfoo\x1b[0m"
	if got := displayWidth(in); got != 3 {
		t.Errorf("displayWidth with ANSI: got %d, want 3", got)
	}
}

func TestDisplayWidth_EmojiIsWidth2(t *testing.T) {
	if got := displayWidth("🧠"); got != 2 {
		t.Errorf("displayWidth(\"🧠\"): got %d, want 2", got)
	}
}

func TestDisplayWidth_BlockBar(t *testing.T) {
	// [████░░] is 6 ASCII brackets/blocks + 0 emoji = width 6.
	// Block elements (U+2588, U+2591) are width 1.
	if got := displayWidth("[████░░]"); got != 8 {
		t.Errorf("displayWidth bar: got %d, want 8", got)
	}
}

func TestDisplayWidth_PowerlineChevron(t *testing.T) {
	// U+E0B1 thin chevron — Nerd Font glyph, narrow (width 1).
	if got := displayWidth(""); got != 1 {
		t.Errorf("displayWidth chevron: got %d, want 1", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestDisplayWidth' -v -count=1`

Expected: compile error — `undefined: displayWidth`.

- [ ] **Step 4: Implement `displayWidth`**

In `internal/pkg/render/render.go`, add at the top of the file's import block:

```go
import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)
```

(Add `"regexp"` and the runewidth import to the existing import block.)

Add the helper near the existing render helpers (anywhere above `Render` is fine):

```go
// ansiRegexp matches ANSI SGR escape sequences so displayWidth can
// strip them before counting visible columns.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// displayWidth returns the visible column count of s after stripping
// ANSI escape sequences. Uses go-runewidth so emoji (width 2), CJK
// fullwidth (width 2), and zero-width joiners are handled correctly.
// The Powerline renderer uses this to know where the bg fill ends.
func displayWidth(s string) int {
	stripped := ansiRegexp.ReplaceAllString(s, "")
	return runewidth.StringWidth(stripped)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestDisplayWidth' -v -count=1`

Expected: all five tests PASS.

Run: `go test -race ./...`

Expected: every package green. `gofmt -l .` empty. `go vet ./...` clean.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): displayWidth helper plus go-runewidth dependency

displayWidth strips ANSI escapes via a package-level regexp and
delegates to go-runewidth.StringWidth so emoji (\"🧠\" = width 2)
and the Powerline chevron (U+E0B1 = width 1) are measured
correctly. Unused for now; the Powerline renderer wires it in two
commits ahead.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Schema upgrade

**Files:**
- Modify: `internal/pkg/render/render.go`
- Modify: `internal/pkg/render/render_test.go`

This task is mechanically intricate. Three interlocked changes:
- Add `Row` struct + custom `UnmarshalJSON`.
- Change `Config.Rows` from `[][]Segment` to `[]Row`.
- Add `Config.Powerline bool` and `renderEnv.ttyCols int`.
- Update `Render`'s inline row loop to iterate `Row`.
- Update *every* existing test that constructs `Config{Rows: [][]Segment{...}}` to `Config{Rows: []Row{{Segments: []Segment{...}}}}`.

- [ ] **Step 1: Write the failing tests for the new schema**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRowUnmarshal_ArrayShape(t *testing.T) {
	// Legacy 0.1.x JSON: row is a bare array of segments.
	data := []byte(`[{"type":"text","label":"A"},{"type":"text","label":"B"}]`)
	var r Row
	if err := r.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if r.Bg != "" {
		t.Errorf("Bg should be empty for array shape, got %q", r.Bg)
	}
	if len(r.Segments) != 2 || r.Segments[0].Label != "A" || r.Segments[1].Label != "B" {
		t.Errorf("Segments: got %#v", r.Segments)
	}
}

func TestRowUnmarshal_ObjectShape(t *testing.T) {
	data := []byte(`{"bg":"234","segments":[{"type":"text","label":"X"}]}`)
	var r Row
	if err := r.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if r.Bg != "234" {
		t.Errorf("Bg: got %q, want %q", r.Bg, "234")
	}
	if len(r.Segments) != 1 || r.Segments[0].Label != "X" {
		t.Errorf("Segments: got %#v", r.Segments)
	}
}

func TestRowUnmarshal_EmptyValueIsError(t *testing.T) {
	var r Row
	if err := r.UnmarshalJSON([]byte("   ")); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestRowUnmarshal_RejectsMalformed(t *testing.T) {
	for _, in := range []string{`null`, `42`, `"string"`, `true`} {
		var r Row
		if err := r.UnmarshalJSON([]byte(in)); err == nil {
			t.Errorf("UnmarshalJSON(%q) should fail", in)
		}
	}
}

func TestConfigJSON_ArrayRowsParseAsLegacy(t *testing.T) {
	// A full Config with the legacy [][]Segment shape must still parse
	// after the upgrade, with each row's Bg empty.
	data := []byte(`{
		"rows": [
			[{"type":"text","label":"A"}],
			[{"type":"text","label":"B"}]
		]
	}`)
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(c.Rows) != 2 {
		t.Fatalf("rows: got %d, want 2", len(c.Rows))
	}
	for i, row := range c.Rows {
		if row.Bg != "" {
			t.Errorf("row %d Bg should be empty, got %q", i, row.Bg)
		}
	}
}

func TestConfigJSON_PowerlineDefault(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(`{}`), &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if c.Powerline {
		t.Error("Powerline should default to false")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestRowUnmarshal|TestConfigJSON' -v -count=1`

Expected: compile error — `undefined: Row`, `unknown field Powerline`.

- [ ] **Step 3: Add `Row` and friends to `render.go`**

In `internal/pkg/render/render.go`, replace the existing `Config` struct with:

```go
// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      []Row  `json:"rows,omitzero"`
	Separator string `json:"separator,omitempty"`
	Powerline bool   `json:"powerline,omitempty"`
}

// Row is one output line. Powerline mode reads Bg to colour-fill the
// row from column 0 to the terminal's right edge; without Powerline
// or with an empty Bg, the row renders as a plain join of its
// segments.
type Row struct {
	Segments []Segment `json:"segments"`
	Bg       string    `json:"bg,omitempty"`
}

// UnmarshalJSON accepts two shapes:
//   - The legacy 0.1.x bare array of segments: [{...},{...}]
//     → unmarshals into Row{Segments: ..., Bg: ""}.
//   - The 0.2.0 native object form: {"segments":[...], "bg":"234"}.
//
// Detection is by the first non-whitespace byte: '[' for array, anything
// else routes to the object decoder.
func (r *Row) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return errors.New("render.Row: empty value")
	}
	if trimmed[0] == '[' {
		var segs []Segment
		if err := json.Unmarshal(trimmed, &segs); err != nil {
			return err
		}
		r.Segments = segs
		r.Bg = ""
		return nil
	}
	// Object shape — use an alias to bypass this UnmarshalJSON method.
	type rowAlias Row
	var a rowAlias
	if err := json.Unmarshal(trimmed, &a); err != nil {
		return err
	}
	*r = Row(a)
	return nil
}
```

Add `"bytes"` and `"errors"` to the import block of `render.go`.

Add a new field to `renderEnv`:

```go
type renderEnv struct {
	cwd          string
	colorEnabled bool
	nowUnix      int64
	ttyCols      int // populated only when Config.Powerline is true and colour is on
}
```

Update `Render`'s inline row loop. The current code:

```go
var lines []string
for _, row := range rows {
	var parts []string
	for _, seg := range row {
		s := renderSegment(&p, seg, env)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, sep))
	}
}
```

becomes:

```go
var lines []string
for _, row := range rows {
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(&p, seg, env)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, sep))
	}
}
```

(Just `row.Segments` instead of `row`.)

Update `defaultRows`:

```go
var defaultRows = []Row{
	{Segments: []Segment{{Type: "model", Show1MFlag: true}, {Type: "context", Style: "bar+pct"}}},
	{Segments: []Segment{{Type: "cost"}, {Type: "limit_5h"}, {Type: "limit_7d"}}},
	{Segments: []Segment{{Type: "git_branch"}, {Type: "cwd"}}},
}
```

- [ ] **Step 4: Rewrite existing in-package test constructions**

`internal/pkg/render/render_test.go` has many `Config{Rows: [][]Segment{...}}` constructions that will no longer compile. Update each occurrence. The mechanical rule: wrap inner `[][]Segment{{seg, seg}, {seg}}` as `[]Row{{Segments: []Segment{seg, seg}}, {Segments: []Segment{seg}}}`.

Affected tests (compile-error each will surface them; this list is for review completeness):

- `TestRender_UnknownSegmentTypeRendersMarker`
- `TestRender_RowsJoinedWithNewline_SegmentsWithSeparator`
- `TestRender_DefaultSeparator`
- `TestRender_ContextThresholdProducesExpectedAnsi`
- `TestRender_StyledEmptySegmentEmitsNothing`
- `TestRender_StyledNonEmptySegmentStillWraps`
- `TestRender_AllTargetStillWorksAsBefore`
- `TestRender_ContextPctTargetColorsOnlyDigits`
- `TestRender_Limit5hPctTargetColorsOnlyDigits`

Sample rewrite for the first one:

Before:
```go
cfg := Config{Rows: [][]Segment{{{Type: "frobnicate"}}}}
```

After:
```go
cfg := Config{Rows: []Row{{Segments: []Segment{{Type: "frobnicate"}}}}}
```

Sample rewrite for `TestRender_RowsJoinedWithNewline_SegmentsWithSeparator`:

Before:
```go
cfg := Config{
	Rows: [][]Segment{
		{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
		{{Type: "text", Label: "C"}},
	},
	Separator: " | ",
}
```

After:
```go
cfg := Config{
	Rows: []Row{
		{Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}}},
		{Segments: []Segment{{Type: "text", Label: "C"}}},
	},
	Separator: " | ",
}
```

Apply the same mechanical translation to every other test. Compile errors will guide you if you miss one.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -v -count=1`

Expected: every test PASSes — both the new schema tests and every existing test (after the mechanical rewrites).

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): Row struct plus Config.Powerline schema upgrade

Rows is now []Row instead of [][]Segment. Row carries Segments and
an optional Bg used by the upcoming Powerline path. Row's custom
UnmarshalJSON accepts both shapes so existing 0.1.x configs parse
unchanged (the bare-array form unmarshals into Row{Bg: \"\"}).
Config gains a top-level Powerline bool (default false). renderEnv
gains ttyCols for the Powerline path to read. All existing
in-package test constructions are rewritten to the new Row form
mechanically.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `renderRowPowerline`

**Files:**
- Modify: `internal/pkg/render/render.go`
- Modify: `internal/pkg/render/render_test.go`

This task adds the function but does NOT wire it into `Render`. Task 5 does the dispatch.

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRenderRowPowerline_AllEmptyReturnsEmpty(t *testing.T) {
	// Row whose every segment renders to "" produces "".
	row := Row{Bg: "234", Segments: []Segment{{Type: "git_branch"}}}
	p := &payload{} // no workspace.current_dir → git_branch renders ""
	env := renderEnv{colorEnabled: true, ttyCols: 80}
	if got := renderRowPowerline(p, row, env); got != "" {
		t.Errorf("all-empty row: got %q, want \"\"", got)
	}
}

func TestRenderRowPowerline_OpensAndClosesRowBg(t *testing.T) {
	row := Row{Bg: "234", Segments: []Segment{{Type: "text", Label: "A"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.HasPrefix(got, "\x1b[48;5;234m") {
		t.Errorf("row must open with row-bg, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("row must end with \\x1b[0m reset, got %q", got)
	}
}

func TestRenderRowPowerline_ChevronBetweenSegments(t *testing.T) {
	row := Row{Bg: "234", Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "text", Label: "B"},
		{Type: "text", Label: "C"},
	}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	// Exactly two chevrons for three segments.
	if n := strings.Count(got, ""); n != 2 {
		t.Errorf("chevron count: got %d, want 2 in %q", n, got)
	}
	// Chevron carries the muted-grey fg.
	if !strings.Contains(got, "\x1b[38;5;245m") {
		t.Errorf("chevron should be wrapped in fg 245, got %q", got)
	}
}

func TestRenderRowPowerline_EmptySegmentDropsChevron(t *testing.T) {
	// Three segments where the middle renders empty → only one chevron.
	row := Row{Bg: "234", Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "git_branch"},        // renders "" (no payload data)
		{Type: "text", Label: "C"},
	}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	if n := strings.Count(got, ""); n != 1 {
		t.Errorf("chevron count: got %d, want 1 in %q", n, got)
	}
}

func TestRenderRowPowerline_FullWidthPaddingWhenTTYKnown(t *testing.T) {
	// ttyCols=80, two-letter segment → bg-fill should reach exactly 80
	// visible columns. The padded line's displayWidth must be 80.
	row := Row{Bg: "234", Segments: []Segment{{Type: "text", Label: "AB"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 80}
	got := renderRowPowerline(&payload{}, row, env)
	if w := displayWidth(got); w != 80 {
		t.Errorf("padded width: got %d, want 80 in %q", w, got)
	}
}

func TestRenderRowPowerline_NoPaddingWhenTTYIsZero(t *testing.T) {
	row := Row{Bg: "234", Segments: []Segment{{Type: "text", Label: "AB"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	// Without ttyCols, padding step is skipped; the visible width is
	// the segment's own width (no trailing spaces).
	if w := displayWidth(got); w != 2 {
		t.Errorf("no-pad width: got %d, want 2 in %q", w, got)
	}
}

func TestRenderRowPowerline_NoBgSkipsBgEscape(t *testing.T) {
	// A row with empty Bg still rendres but never emits \x1b[48;...
	row := Row{Bg: "", Segments: []Segment{{Type: "text", Label: "A"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 80}
	got := renderRowPowerline(&payload{}, row, env)
	if strings.Contains(got, "\x1b[48;") {
		t.Errorf("empty Bg should not emit bg escape, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestRenderRowPowerline' -v -count=1`

Expected: compile error — `undefined: renderRowPowerline`.

- [ ] **Step 3: Implement `renderRowPowerline`**

Add to `internal/pkg/render/render.go` (near the existing `renderSegment` / `chooseFG` cluster). Also add the chevron constants near the top of the file (anywhere above this function):

```go
const (
	powerlineChevron      = ""
	powerlineChevronWidth = 1
	powerlineChevronFG    = "245"
)
```

```go
// renderRowPowerline builds one Powerline-styled row: row-bg fill,
// thin chevrons between non-empty segments, full-width padding when
// the TTY column count is known.
//
// The row-bg must be re-emitted after every segment because each
// segment's outer style() wrap ends with \x1b[0m, which resets both
// fg AND bg. Re-emitting bg256(row.Bg) between segments and before
// the padding step keeps the bar visually continuous.
func renderRowPowerline(p *payload, row Row, env renderEnv) string {
	// 1. Render each segment; drop empties.
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

	// 2. Build the chevron: muted-grey fg, no bg of its own, closed
	//    with the default-foreground SGR so the surrounding bg
	//    continues to show through.
	chev := powerlineChevron
	if open := fg256(powerlineChevronFG); open != "" {
		chev = open + powerlineChevron + "\x1b[39m"
	}

	// 3. bgOpen is re-emitted after each [0m-terminated segment.
	var bgOpen string
	if row.Bg != "" {
		bgOpen = bg256(row.Bg)
	}

	// 4. Interleave.
	var b strings.Builder
	b.WriteString(bgOpen)
	for i, part := range parts {
		if i > 0 {
			b.WriteString(chev)
			b.WriteString(bgOpen) // chev was fg-only; ensure bg before next segment
		}
		b.WriteString(part)
		b.WriteString(bgOpen) // segment's [0m killed bg; restore before chev/padding/reset
	}

	// 5. Pad to terminal width.
	if env.ttyCols > 0 && row.Bg != "" {
		used := 0
		for _, part := range parts {
			used += displayWidth(part)
		}
		used += (len(parts) - 1) * powerlineChevronWidth
		if remaining := env.ttyCols - used; remaining > 0 {
			b.WriteString(strings.Repeat(" ", remaining))
		}
	}

	// 6. Close.
	if row.Bg != "" {
		b.WriteString(reset)
	}
	return b.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestRenderRowPowerline' -v -count=1`

Expected: all seven `TestRenderRowPowerline_*` tests PASS.

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): renderRowPowerline with row-bg fill and chevron interleave

The Powerline row builder: opens the row-bg once, walks the
non-empty segments interleaving the U+E0B1 thin chevron (fg 245,
default-foreground closer so the bg shows through), re-establishes
the row-bg after every segment to compensate for style()'s
\\x1b[0m reset, and pads the line to env.ttyCols when known.
Unwired so far; Render's dispatch lands in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: `Render` dispatch

**Files:**
- Modify: `internal/pkg/render/render.go`
- Modify: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_PowerlineFalseUsesNaturalPath(t *testing.T) {
	// With Powerline=false, the output must look like 0.1.x — no
	// row-bg, no chevron, configured separator between segments.
	cfg := Config{
		Powerline: false,
		Rows: []Row{{Bg: "234", Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		}}},
		Separator: " | ",
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "A | B") {
		t.Errorf("natural path should join with separator, got %q", got)
	}
	if strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("natural path must not emit row-bg, got %q", got)
	}
	if strings.Contains(got, "") {
		t.Errorf("natural path must not emit chevron, got %q", got)
	}
}

func TestRender_PowerlineTrueEmitsBgAndChevron(t *testing.T) {
	cfg := Config{
		Powerline: true,
		Rows: []Row{{Bg: "234", Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		}}},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("Powerline path must emit row-bg, got %q", got)
	}
	if !strings.Contains(got, "") {
		t.Errorf("Powerline path must emit chevron, got %q", got)
	}
}

func TestRender_PowerlineNoColorFallsBackToNatural(t *testing.T) {
	cfg := Config{
		Powerline: true,
		Rows: []Row{{Bg: "234", Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
		}}},
		Separator: " | ",
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "A | B") {
		t.Errorf("NoColor + Powerline must use natural separator, got %q", got)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("NoColor must emit no ANSI, got %q", got)
	}
	if strings.Contains(got, "") {
		t.Errorf("NoColor + Powerline must not emit chevron, got %q", got)
	}
}

func TestRender_PowerlineTwoRowsDifferentBgs(t *testing.T) {
	cfg := Config{
		Powerline: true,
		Rows: []Row{
			{Bg: "234", Segments: []Segment{{Type: "text", Label: "row1"}}},
			{Bg: "237", Segments: []Segment{{Type: "text", Label: "row2"}}},
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("row 1 must use bg 234, got %q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;237m") {
		t.Errorf("row 2 must use bg 237, got %q", got)
	}
}

func TestRender_PowerlineTTYColsPropagated(t *testing.T) {
	// Swap ttyColsFunc to a deterministic fake; verify the resulting
	// row reaches that exact display width.
	prev := ttyColsFunc
	defer func() { ttyColsFunc = prev }()
	ttyColsFunc = func() int { return 40 }

	cfg := Config{
		Powerline: true,
		Rows:      []Row{{Bg: "234", Segments: []Segment{{Type: "text", Label: "hi"}}}},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if w := displayWidth(got); w != 40 {
		t.Errorf("display width: got %d, want 40 in %q", w, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestRender_Powerline' -v -count=1`

Expected: the Powerline-true tests FAIL because dispatch is not yet wired (current `Render` always uses the inline join path). `TestRender_PowerlineFalseUsesNaturalPath` may PASS already.

- [ ] **Step 3: Extract `renderRowNatural` and dispatch in `Render`**

In `internal/pkg/render/render.go`, replace the existing inline row loop in `Render` with a switched dispatch.

Before (current `Render`):

```go
env := renderEnv{
	cwd:          cwd,
	colorEnabled: !opts.NoColor,
	nowUnix:      nowFunc().Unix(),
}

var lines []string
for _, row := range rows {
	var parts []string
	for _, seg := range row.Segments {
		s := renderSegment(&p, seg, env)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, sep))
	}
}
return strings.Join(lines, "\n"), nil
```

After:

```go
env := renderEnv{
	cwd:          cwd,
	colorEnabled: !opts.NoColor,
	nowUnix:      nowFunc().Unix(),
}
if opts.Config.Powerline && env.colorEnabled {
	env.ttyCols = ttyColsFunc()
}

var lines []string
for _, row := range rows {
	var line string
	if opts.Config.Powerline && env.colorEnabled {
		line = renderRowPowerline(&p, row, env)
	} else {
		line = renderRowNatural(&p, row, env, sep)
	}
	if line != "" {
		lines = append(lines, line)
	}
}
return strings.Join(lines, "\n"), nil
```

Add the extracted `renderRowNatural` function (place it next to `renderRowPowerline`):

```go
// renderRowNatural is the 0.1.x row builder: render each non-empty
// segment, join them with the configured separator. Returns "" when
// every segment renders empty so the row joiner skips the row.
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestRender_Powerline' -v -count=1`

Expected: all five new `TestRender_Powerline_*` tests PASS.

Run: `go test -race ./...`

Expected: every package green, including the existing golden-fixture suite (it does not set `Powerline: true`, so it routes through `renderRowNatural`).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): dispatch Powerline vs natural row path in Render

When Config.Powerline is true and colour is enabled, each row goes
through renderRowPowerline; ttyColsFunc is queried once per Render
to populate renderEnv.ttyCols for the bg-fill. Otherwise the
extracted renderRowNatural keeps the 0.1.x behaviour bit-for-bit.
NO_COLOR routes through renderRowNatural unconditionally, which is
the predictable plain-text fallback.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Universal space-after-colon plus golden regeneration

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/render_test.go`
- Modify: `internal/pkg/render/testdata/golden/*.txt`

- [ ] **Step 1: Write the failing tests for each affected segment**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRenderEffort_LabelColonSpacing(t *testing.T) {
	p := &payload{Effort: effortF{Level: "xhigh"}}
	got := renderEffort(p, Segment{Type: "effort"}, renderEnv{})
	if got != "effort: xhigh" {
		t.Errorf("default label: got %q, want %q", got, "effort: xhigh")
	}
	got = renderEffort(p, Segment{Type: "effort", Label: "eff"}, renderEnv{})
	if got != "eff: xhigh" {
		t.Errorf("custom label: got %q, want %q", got, "eff: xhigh")
	}
}

func TestRenderOutputStyle_LabelColonSpacing(t *testing.T) {
	p := &payload{OutputStyle: outputF{Name: "concise"}}
	got := renderOutputStyle(p, Segment{Type: "output_style"}, renderEnv{})
	if got != "style: concise" {
		t.Errorf("got %q, want %q", got, "style: concise")
	}
}

func TestRenderLimit5h_LabelColonSpacing(t *testing.T) {
	p := &payload{Limits: limitsF{FiveHour: rateLimitF{UsedPercentage: 51, ResetsAt: 1000}}}
	got := renderLimit5h(p, Segment{Type: "limit_5h"}, renderEnv{nowUnix: 0})
	// The countdown text varies; assert the prefix only.
	if !strings.HasPrefix(got, "5h: 51%") {
		t.Errorf("got %q, want prefix %q", got, "5h: 51%")
	}
}

func TestRenderLimit7d_LabelColonSpacing(t *testing.T) {
	p := &payload{Limits: limitsF{SevenDay: rateLimitF{UsedPercentage: 74, ResetsAt: 1000}}}
	got := renderLimit7d(p, Segment{Type: "limit_7d"}, renderEnv{nowUnix: 0})
	if !strings.HasPrefix(got, "7d: 74%") {
		t.Errorf("got %q, want prefix %q", got, "7d: 74%")
	}
}

func TestRenderGitBranch_LabelColonSpacing(t *testing.T) {
	// branch() depends on filesystem state; we exercise just the label
	// formatting by faking the renderEnv.cwd to "" so branch() returns
	// "" and the label-only path is never reached. Instead, drive
	// through Render() with a known-good cwd is heavier; for unit-level
	// coverage we test the formatting directly by tempting fate with
	// the project's own .git. If the test runner's cwd has a git repo,
	// the branch is non-empty and the label format is exercised.
	//
	// Simpler: build a tiny fake by wrapping renderGitBranch with a
	// non-empty Label and a renderEnv whose cwd points at the
	// repository root.
	got := renderGitBranch(&payload{}, Segment{Type: "git_branch", Label: "git"}, renderEnv{cwd: "."})
	if got == "" {
		t.Skip("not in a git repo or branch detection failed; not a regression test")
	}
	if !strings.HasPrefix(got, "git: ") {
		t.Errorf("label format: got %q, want prefix %q", got, "git: ")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestRender.*LabelColonSpacing|TestRenderEffort|TestRenderOutputStyle|TestRenderLimit5h|TestRenderLimit7d|TestRenderGitBranch' -v -count=1`

Expected: every test FAILs because the existing format strings produce `effort:xhigh`, `style:concise`, `5h:51%`, `7d:74%`, `git:main`.

- [ ] **Step 3: Apply the spacing fix in `segments.go`**

In `internal/pkg/render/segments.go`:

Line 72 — `renderEffort`:

```go
return label + ": " + p.Effort.Level
```

Line 90 — `renderOutputStyle`:

```go
return label + ": " + p.OutputStyle.Name
```

Line 251 — `renderLimit` case `"bar"`:

```go
return fmt.Sprintf("%s: %s", label, makeBar(rl.UsedPercentage, barCells))
```

Line 254 — `renderLimit` case `"bar+pct"` no resets_at:

```go
return fmt.Sprintf("%s: %s %s", label, makeBar(rl.UsedPercentage, barCells), pct)
```

Line 256 — `renderLimit` case `"bar+pct"` with resets_at:

```go
return fmt.Sprintf("%s: %s %s (%s)", label, makeBar(rl.UsedPercentage, barCells), pct, formatCountdown(rl.ResetsAt-env.nowUnix))
```

Line 259 — `renderLimit` default no resets_at:

```go
return fmt.Sprintf("%s: %s", label, pct)
```

Line 261 — `renderLimit` default with resets_at:

```go
return fmt.Sprintf("%s: %s (%s)", label, pct, formatCountdown(rl.ResetsAt-env.nowUnix))
```

Line 329 — `renderGitBranch`:

```go
return s.Label + ": " + b
```

- [ ] **Step 4: Regenerate the golden fixtures**

Run: `go test ./internal/pkg/render -run 'TestRender_GoldenFixtures' -update`

Expected: the five files under `internal/pkg/render/testdata/golden/` are rewritten with the new spacing (`5h: N%`, `7d: N%`, etc.). Verify with:

```bash
git diff -- internal/pkg/render/testdata/golden/
```

The diff should show only `5h:N%` → `5h: N%` and `7d:N%` → `7d: N%` substitutions (and similar `(countdown)` repositioning) — no other content changes.

- [ ] **Step 5: Run the full suite**

Run: `go test -race ./...`

Expected: every package green. The newly regenerated goldens are pinned by `TestRender_GoldenFixtures`; if they fail, something other than spacing also changed and must be investigated.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/render_test.go internal/pkg/render/testdata/golden/
git commit -s -m "feat(render): universal space after colon in label-bearing segments

5h: 51%, 7d: 74%, effort: xhigh, style: concise, git: main —
consistent space after the colon across renderEffort,
renderOutputStyle, renderLimit, and renderGitBranch. Five golden
fixtures are regenerated to pin the new spacing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Documentation

**Files:**
- Modify: `docs/configuration.md`

- [ ] **Step 1: Document the Row shape**

Find the `## render` section. After the existing `### Fields` table (which lists `rows` and `separator`), insert a new subsection just before `### Last-resort fallback`:

````markdown
### Row shape

Each entry in `rows` is either:

- **Legacy array form** (0.1.x compatibility) — a bare array of
  segments. The row has no background and is rendered with the
  configured `separator`:

  ```json
  [{"type": "model"}, {"type": "context", "style": "bar+pct"}]
  ```

- **Object form** (0.2.0 native) — `{"segments": [...], "bg": "234"}`.
  The `bg` field is meaningful only when `powerline` is true; it
  fills the row from column 0 to the terminal's right edge in
  Powerline mode and is ignored otherwise.

Both shapes can be mixed within the same `rows` array. `ccsb`
writes back the object form whenever it persists the config (e.g.,
on `ccsb install` or `ccsb mode`), so a legacy config is migrated
automatically the next time ccsb modifies the file.
````

- [ ] **Step 2: Document the `powerline` toggle**

In the same `## render` section, extend the `### Fields` table to add a row for `powerline`. The current rows are:

```markdown
| `rows`      | array of arrays | default rows  | …                  |
| `separator` | string          | `" \| "`      | …                  |
```

Add (after `separator`):

```markdown
| `powerline` | bool            | `false`       | When true, each row's `bg` fills the terminal width and segments are joined with the U+E0B1 thin chevron. See [Powerline](#powerline). |
```

- [ ] **Step 3: Add the `### Powerline` subsection**

At the end of the `## render` section (just before `## Segments`), add:

````markdown
### Powerline

With `powerline: true`, the renderer switches to a Powerline-style
row layout:

- Each `Row.Bg` is opened at the start of the row and fills the
  line all the way to the terminal's right edge (detected via
  `/dev/tty + ioctl(TIOCGWINSZ)`; falls back to natural width when
  no controlling tty is available).
- Segments are joined with the U+E0B1 thin chevron in a muted-grey
  foreground (`245`). The chevron has no background of its own, so
  the row's background shows through.
- Per-segment `fg` / `bg` / `bold` continue to apply *inside* the
  row-bg. A segment with its own `bg` overrides the row-bg for
  that segment's text.
- Empty segments (e.g. `mode` when neither thinking nor fast_mode
  is set) are dropped together with the surrounding chevron, so
  the chain stays seamless.
- With `NO_COLOR`, Powerline degrades to the natural-separator
  path: no bg, no chevron, segments joined with the configured
  `separator`. The output is identical to a plain 0.1.x render.

Example two-row config with a two-tone palette:

```json
{
  "render": {
    "powerline": true,
    "rows": [
      {"bg": "234", "segments": [
        {"type": "model", "fg": "33", "bold": true, "show_1m_flag": true},
        {"type": "mode"},
        {"type": "context", "style": "bar+pct", "fg": "245",
         "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_5h", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_7d", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ]},
      {"bg": "237", "segments": [
        {"type": "git_branch", "fg": "33"},
        {"type": "lines", "fg": "245"},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
```

The chevron glyph and its foreground are hardcoded in 0.2.0; future
releases may expose them as config knobs.
````

- [ ] **Step 4: Update per-segment entries for the new spacing**

Find each segment doc and update its example to reflect the new spacing. Replace each `<label>:<value>` example with `<label>: <value>`:

In `#### `effort``:

```markdown
{"type": "effort"}                     // "effort: xhigh"
{"type": "effort", "label": "eff"}     // "eff: xhigh"
```

In `#### `output_style``:

```markdown
{"type": "output_style"}               // "style: concise"
```

In `#### `limit_5h`, `limit_7d``, update the example output `5h:18% (2h15m)` → `5h: 18% (2h15m)` and table rows:

```markdown
| `""` / `"pct"` | `5h: 18% (2h15m)` (default)                  |
| `"bar"`     | `5h: [███░░░░░░░░░░░░░]`                        |
| `"bar+pct"` | `5h: [███░░░░░░░░░░░░░] 18% (2h15m)`            |
```

In `#### `git_branch``:

```markdown
{"type": "git_branch", "label": "git"} // "git: main"
```

- [ ] **Step 5: Verify the diff is doc-only**

Run: `git diff --stat docs/configuration.md`

Expected: only `docs/configuration.md` is modified.

Run: `go test -race ./...`

Expected: still green (this task is doc-only, but the run confirms nothing accidentally changed).

- [ ] **Step 6: Commit**

```bash
git add docs/configuration.md
git commit -s -m "docs(render): document Powerline plus the new Row shape

Adds Row-shape documentation (legacy array vs new object form),
the powerline top-level toggle, a dedicated Powerline subsection
covering the chevron + bg-fill semantics and NO_COLOR fallback, a
worked two-tone two-row example, and updates per-segment
<label>:<value> examples to the new space-after-colon spacing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

After Task 7, the branch is feature-complete. Before squashing into `development-0.2.0-main`:

- [ ] Run the full suite once more: `go test -race -cover ./...` — every package PASSes; render-package coverage stays at ≥ 95.0 %.
- [ ] Lint: `go vet ./...` produces no output; `gofmt -l .` is empty.
- [ ] Manual smoke test against the user's live capture corpus:

```bash
go build -o bin/ccsb ./cmd/ccsb
TMP_CFG=$(mktemp -d)
mkdir -p "$TMP_CFG/ccsb"
cat > "$TMP_CFG/ccsb/config.json" <<'EOF'
{
  "render": {
    "powerline": true,
    "rows": [
      {"bg": "234", "segments": [
        {"type": "model", "fg": "33", "bold": true, "show_1m_flag": true},
        {"type": "mode"},
        {"type": "context", "style": "bar+pct", "fg": "245",
         "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_5h", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]},
        {"type": "limit_7d", "fg": "245", "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ]},
      {"bg": "237", "segments": [
        {"type": "git_branch", "fg": "33"},
        {"type": "lines", "fg": "245"},
        {"type": "cwd", "fg": "245"}
      ]}
    ]
  }
}
EOF
latest=$(ls -1t ~/.local/state/ccsb/captures/*.json | head -1)
XDG_CONFIG_HOME=$TMP_CFG ./bin/ccsb < "$latest"
```

Expected: two-row Powerline output with bg 234 / bg 237 rows, U+E0B1 chevrons between segments, full-line bg fills to terminal width. `5h: NN%` / `7d: NN%` show the new spacing. With `NO_COLOR=1 XDG_CONFIG_HOME=$TMP_CFG ./bin/ccsb < "$latest"`, the output drops back to the natural-separator path with no ANSI bytes and no chevron.

## Out of scope (reminder)

The spec explicitly excludes these — do not add them:

- Right-alignment (`align: "right"`), per-segment truncation (`max_width`),
  or `min_cols`-based suppression. All deferred to 0.2.1.
- Configurable chevron glyph or chevron-fg. Hardcoded for 0.2.0.
- 24-bit color support. Continues to use ANSI 256-color codes.
- A `tty_size` debug segment.
- Any change to `wrapPct`, `chooseFG`, `threshold_target`, or the empty-segment-drop behaviour from 0.1.11/0.1.12 — all orthogonal to Powerline and unchanged.

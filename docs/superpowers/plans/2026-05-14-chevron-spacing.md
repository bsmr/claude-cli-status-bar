# Chevron Spacing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a single space on each side of the Powerline chevron so the thin-glyph separator no longer reads as cramped, and keep the full-width bg-fill math in sync with the new join cost.

**Architecture:** Two-step TDD. First cycle adds the visual spacing in `renderRowPowerline`'s interleave loop and asserts it via a new test against ANSI-stripped output. Second cycle introduces the `powerlineSeparatorWidth` constant and threads it through the full-width padding math, asserting the corrected math via a multi-segment + ttyCols test that the first cycle would otherwise leave broken. Documentation bullet update and verification gate close out.

**Tech Stack:** Go, package `go.muehmer.eu/claude-cli-status-bar/internal/pkg/render` (in-package tests), no new dependencies.

**Branch:** `development-0.2.1-work` already exists with spec commit `801b515` as the only commit beyond `main`. Plan executes on this branch.

---

## File Structure

- `internal/pkg/render/render.go` — Powerline constants block + `renderRowPowerline` interleave loop + padding math. All edits in this one file for production code.
- `internal/pkg/render/render_test.go` — two new test functions appended near the existing `TestRenderRowPowerline_*` block.
- `docs/configuration.md` — one bullet under the `### Powerline` section.

No new files, no new packages, no new dependencies.

---

## Task 1: Add chevron spacing in the interleave loop

**Files:**
- Modify: `internal/pkg/render/render.go` (function `renderRowPowerline`, step 4)
- Test: `internal/pkg/render/render_test.go` (append new test)

### Step 1: Write the failing test

Append to `internal/pkg/render/render_test.go` after `TestRenderRowPowerline_NoColorReturnsEmpty` (which currently ends near line 722):

```go
func TestRenderRowPowerline_ChevronHasSpacingAroundIt(t *testing.T) {
	// Three single-letter segments with no row bg so the only
	// separators in the visible output are " <chev> ".
	row := Row{Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "text", Label: "B"},
		{Type: "text", Label: "C"},
	}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	stripped := ansiRegexp.ReplaceAllString(got, "")
	want := " " + powerlineChevron + " "
	if n := strings.Count(stripped, want); n != 2 {
		t.Errorf("expected %q to appear 2 times in stripped output, got %d\nstripped: %q", want, n, stripped)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestRenderRowPowerline_ChevronHasSpacingAroundIt ./internal/pkg/render -v`

Expected output: `--- FAIL: TestRenderRowPowerline_ChevronHasSpacingAroundIt` with the error message reporting `got 0` (the current renderer writes the chevron flush, so ` <chev> ` never occurs in the stripped output).

- [ ] **Step 3: Add the spacing in `renderRowPowerline`**

Open `internal/pkg/render/render.go` and locate the interleave loop in `renderRowPowerline` (currently around line 416). Replace the existing block:

```go
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
```

with:

```go
	// 4. Interleave with a single space on each side of the chevron
	//    so the thin glyph has breathing room from the neighbouring
	//    segment text. The spaces inherit the row bg from the
	//    previous bgOpen re-emission and the chevron only flips fg,
	//    so the bg stays continuous across the " <chev> " cell.
	var b strings.Builder
	b.WriteString(bgOpen)
	for i, part := range parts {
		if i > 0 {
			b.WriteString(" ")
			b.WriteString(chev)
			b.WriteString(" ")
			b.WriteString(bgOpen) // chev was fg-only; ensure bg before next segment
		}
		b.WriteString(part)
		b.WriteString(bgOpen) // segment's [0m killed bg; restore before chev/padding/reset
	}
```

- [ ] **Step 4: Run the new test to verify it passes**

Run: `go test -run TestRenderRowPowerline_ChevronHasSpacingAroundIt ./internal/pkg/render -v`

Expected output: `--- PASS: TestRenderRowPowerline_ChevronHasSpacingAroundIt`.

- [ ] **Step 5: Run the full render-package suite to confirm no regression**

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS. `TestRenderRowPowerline_FullWidthPaddingWhenTTYKnown` still passes because it uses a single-segment row (zero separators, math unchanged). `TestRenderRowPowerline_ChevronBetweenSegments` still passes because it only counts chevrons.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
fix(render): pad Powerline chevron with a space on each side

The thin U+E0B1 chevron sat flush against the neighbouring segment
text and read as cramped. Insert a single space before and after
the chevron in renderRowPowerline's interleave loop so the
separator has breathing room.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Update the full-width padding math

**Files:**
- Modify: `internal/pkg/render/render.go` (constants block + `renderRowPowerline` step 5)
- Test: `internal/pkg/render/render_test.go` (append new test)

### Step 1: Write the failing test

Append to `internal/pkg/render/render_test.go` immediately after `TestRenderRowPowerline_ChevronHasSpacingAroundIt` from Task 1:

```go
func TestRenderRowPowerline_MultiSegmentPaddingHonoursSpacing(t *testing.T) {
	// Three single-letter segments + row bg + ttyCols=80 means the
	// visible content before padding is 3 segs + 2 separators of
	// width 3 ("<sp><chev><sp>") = 9 cols. The pad step must fill the
	// remaining 71 cols so displayWidth(got) == 80 exactly.
	row := Row{Bg: "234", Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "text", Label: "B"},
		{Type: "text", Label: "C"},
	}}
	env := renderEnv{colorEnabled: true, ttyCols: 80}
	got := renderRowPowerline(&payload{}, row, env)
	if w := displayWidth(got); w != 80 {
		t.Errorf("padded width: got %d, want 80\noutput: %q", w, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestRenderRowPowerline_MultiSegmentPaddingHonoursSpacing ./internal/pkg/render -v`

Expected: `--- FAIL`. After Task 1 the interleave emits 9 visible cols of content, but the math still computes `used = 3 + 2*1 = 5`, pads with 75 spaces, total 84. The error message reports `got 84, want 80`.

- [ ] **Step 3: Add the `powerlineSeparatorWidth` constant**

In `internal/pkg/render/render.go`, locate the Powerline constants block (currently around lines 198-202):

```go
const (
	powerlineChevron      = ""
	powerlineChevronWidth = 1
	powerlineChevronFG    = "245"
)
```

Replace with:

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

- [ ] **Step 4: Use the new constant in the padding math**

In `internal/pkg/render/render.go`, locate the padding step in `renderRowPowerline` (currently around line 433):

```go
		used += (len(parts) - 1) * powerlineChevronWidth
```

Replace with:

```go
		used += (len(parts) - 1) * powerlineSeparatorWidth
```

- [ ] **Step 5: Run the new test to verify it passes**

Run: `go test -run TestRenderRowPowerline_MultiSegmentPaddingHonoursSpacing ./internal/pkg/render -v`

Expected: `--- PASS`.

- [ ] **Step 6: Run the full render-package suite to confirm no regression**

Run: `go test ./internal/pkg/render -v`

Expected: all tests PASS. `TestRenderRowPowerline_FullWidthPaddingWhenTTYKnown` (single-segment) is unaffected because `len(parts)-1 == 0` zeroes out the separator term regardless of the per-separator width.

- [ ] **Step 7: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
fix(render): tie full-width padding math to chevron separator width

Introduce powerlineSeparatorWidth = powerlineChevronWidth + 2 and
use it in the padding step so the bg-fill accounts for the spaces
added around the chevron. powerlineChevronWidth keeps meaning the
bare glyph width.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Documentation refresh

**Files:**
- Modify: `docs/configuration.md` (Powerline section bullet about the chevron)

### Step 1: Edit the bullet

Open `docs/configuration.md` and locate the bullet (currently lines 97-99) under `### Powerline`:

```
- Segments are joined with the U+E0B1 thin chevron in a muted-grey
  foreground (`245`). The chevron has no background of its own, so
  the row's background shows through.
```

Replace with:

```
- Segments are joined with the U+E0B1 thin chevron in a muted-grey
  foreground (`245`), with a single space on each side for breathing
  room. The chevron has no background of its own, so the row's
  background shows through the spaces and the glyph.
```

- [ ] **Step 2: Confirm the change**

Run: `grep -n "single space on each side" docs/configuration.md`

Expected: a single line of output reporting the matched line under the `### Powerline` section.

- [ ] **Step 3: Commit**

```bash
git add docs/configuration.md
git commit -s -m "$(cat <<'EOF'
docs(configuration): note the space padding around the Powerline chevron

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Final verification gate

**Files:** none modified — verification only.

- [ ] **Step 1: Format check**

Run: `gofmt -l .`

Expected: empty output (no mis-formatted files).

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`

Expected: empty output (no vet warnings).

- [ ] **Step 3: Race + coverage suite**

Run: `go test -race -cover ./...`

Expected: every package PASS. Render-package coverage at or above the pre-task baseline (currently around 94.3%; this patch adds two tests and three lines of production code, so coverage should hold steady or rise slightly).

- [ ] **Step 4: Smoke test the binary**

Run:

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"session_id":"x","model":{"display_name":"S"},"workspace":{"current_dir":"/tmp"}}' \
  | XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) ./bin/ccsb \
  | cat -A
```

This invokes the fallback render with no proxy and the default natural-mode rows (Powerline disabled in `defaultRows`), so the chevron does not appear and the output remains identical to 0.2.0 in the natural path. Expected: no errors, single rendered line on stdout, exit code 0.

- [ ] **Step 5: Log branch state for handoff**

Run: `git log main..HEAD --oneline`

Expected: four commits on `development-0.2.1-work`:

1. `801b515` `docs: spec for 0.2.1 chevron-spacing fix` (already present at plan start)
2. The Task 1 spacing commit
3. The Task 2 padding-math commit
4. The Task 3 docs commit

If everything is green, the subagent-driven-development controller hands back to the user, who then runs the three release gates (squash → no-ff merge → production tag + push) per project memory `release_workflow_gates`. Those gates are out of scope for this plan.

---

## Notes for the implementer

- All tests live in `package render` (in-package, not `_test`). New tests append to `internal/pkg/render/render_test.go`.
- `ansiRegexp` and `displayWidth` are already defined in `internal/pkg/render/render.go` and used by other tests in the suite. Do not redefine them.
- `powerlineChevron` evaluates to the literal U+E0B1 byte sequence `\xee\x82\xb1`. Always reference the constant, never write the raw glyph in test source — Go tool encoding can drop bytes during transmission.
- Do not touch the natural-mode renderer (`renderRowNatural`), the golden fixtures (none use Powerline), or the existing eight `TestRenderRowPowerline_*` cases. The spec's tracking table calls them out as unchanged.
- Do not skip hooks, do not amend commits, do not push to remotes — release gates happen after this plan and are user-driven.

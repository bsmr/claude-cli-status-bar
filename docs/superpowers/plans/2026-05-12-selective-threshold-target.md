# Selective threshold target implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `Segment.ThresholdTarget` for 0.1.12 so percentage-bearing segments can scope their threshold-driven color escalation to just the percentage digits, leaving bar cells, tokens, and labels in the segment's static foreground.

**Architecture:** Add the new field to the `Segment` schema. Introduce a `wrapPct` helper in `render.go` that wraps a percentage substring in the threshold-chosen foreground and closes back to the segment's static foreground. `renderSegment` skips its outer `chooseFG` call when the segment is self-styling (`ThresholdTarget == "pct"`). `renderContext` and `renderLimit` route their percentage substrings through `wrapPct`. The "segments MUST NOT emit ANSI" rule in `segments.go` is relaxed to permit sub-region styling with mandatory closing.

**Tech Stack:** Go 1.26, standard library only. Tests use the existing in-package `package render` testing pattern (no separate `_test` package — tests have access to unexported `payload`, `chooseFG`, `wrapPct`, etc.).

**Reference spec:** `docs/superpowers/specs/2026-05-12-selective-threshold-target-design.md`.

## File structure

- Modify: `internal/pkg/render/render.go` — new `Segment.ThresholdTarget` field; new `wrapPct` helper; `renderSegment` dispatch tweak.
- Modify: `internal/pkg/render/render_test.go` — five unit tests for `wrapPct`, two integration tests through `Render()`, one regression test for the `"all"` default.
- Modify: `internal/pkg/render/segments.go` — `renderContext` and `renderLimit` call `wrapPct`; segmentFunc doc comment relaxed; `renderLimit` signature extended to take `p *payload`.
- Modify: `docs/configuration.md` — `threshold_target` field row, updated Thresholds subsection, per-segment cross references.

No new files. Render-package coverage should stay at or above the current 95.0% line.

---

## Task 1: `ThresholdTarget` field and `wrapPct` helper

**Files:**
- Modify: `internal/pkg/render/render.go` (Segment struct, new helper)
- Modify: `internal/pkg/render/render_test.go` (five unit tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/pkg/render/render_test.go`:

```go
func TestWrapPct_AllTargetIsNoOp(t *testing.T) {
	s := Segment{
		Type: "context",
		FG:   "245",
		Thresholds: []Threshold{
			{Min: 90, FG: "160"},
		},
		// ThresholdTarget omitted → defaults to "all".
	}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	if got := wrapPct("95%", s, p, true); got != "95%" {
		t.Errorf("got %q, want raw text", got)
	}
}

func TestWrapPct_PctTargetWithoutMatchIsNoOp(t *testing.T) {
	s := Segment{
		Type:            "context",
		FG:              "245",
		ThresholdTarget: "pct",
		Thresholds: []Threshold{
			{Min: 90, FG: "160"},
		},
	}
	// 50% is below the 90 threshold → no override → no wrap.
	p := &payload{Context: contextF{UsedPercentage: 50}}
	if got := wrapPct("50%", s, p, true); got != "50%" {
		t.Errorf("got %q, want raw text", got)
	}
}

func TestWrapPct_PctTargetMatchWithStaticFG(t *testing.T) {
	s := Segment{
		Type:            "context",
		FG:              "245",
		ThresholdTarget: "pct",
		Thresholds: []Threshold{
			{Min: 90, FG: "160"},
		},
	}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	want := "\x1b[38;5;160m95%\x1b[38;5;245m"
	if got := wrapPct("95%", s, p, true); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapPct_PctTargetMatchWithEmptyFG(t *testing.T) {
	s := Segment{
		Type:            "context",
		FG:              "", // no static foreground
		ThresholdTarget: "pct",
		Thresholds: []Threshold{
			{Min: 90, FG: "160"},
		},
	}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	// Closer falls back to terminal-default SGR.
	want := "\x1b[38;5;160m95%\x1b[39m"
	if got := wrapPct("95%", s, p, true); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapPct_NoColorReturnsRawText(t *testing.T) {
	s := Segment{
		Type:            "context",
		FG:              "245",
		ThresholdTarget: "pct",
		Thresholds: []Threshold{
			{Min: 90, FG: "160"},
		},
	}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	if got := wrapPct("95%", s, p, false); got != "95%" {
		t.Errorf("got %q, want raw text", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkg/render -run 'TestWrapPct' -v -count=1`

Expected: compile error — `undefined: wrapPct` and `unknown field ThresholdTarget`.

- [ ] **Step 3: Add `ThresholdTarget` to the Segment schema**

In `internal/pkg/render/render.go`, the `Segment` struct currently ends with the `Thresholds` field. Add `ThresholdTarget` directly after it:

```go
// Segment is one element in a row. Type drives dispatch; the remaining
// fields are interpreted per-type.
type Segment struct {
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
	Label  string `json:"label,omitempty"`
	FG     string `json:"fg,omitempty"`
	BG     string `json:"bg,omitempty"`
	Bold   bool   `json:"bold,omitempty"`

	// Type-specific knobs:
	Show1MFlag bool   `json:"show_1m_flag,omitempty"` // type=model
	Style      string `json:"style,omitempty"`        // type=context|limit_5h|limit_7d

	// Thresholds let percentage-bearing segments (context, limit_5h,
	// limit_7d) override FG based on the current used-percentage. The
	// highest matching Min wins; thresholds with empty FG are skipped;
	// segments without a percentage metric ignore the field.
	Thresholds []Threshold `json:"thresholds,omitempty"`

	// ThresholdTarget selects which part of the segment is colored when
	// a threshold matches. "" or "all" (default) wraps the whole
	// segment via the renderer's outer style() call. "pct" causes the
	// segment to color only the percentage digits internally, leaving
	// bar cells, tokens, and labels in the segment's static FG.
	// Unknown values fall back to "all".
	ThresholdTarget string `json:"threshold_target,omitempty"`
}
```

- [ ] **Step 4: Add the `wrapPct` helper**

Still in `internal/pkg/render/render.go`, append `wrapPct` immediately after the existing `segmentMetric` function:

```go
// wrapPct conditionally wraps pctText in the threshold-chosen
// foreground when Segment.ThresholdTarget == "pct" and a threshold
// matches. The wrap closes back to the segment's static FG (or to
// "\x1b[39m" terminal-default if the segment has no static FG) so
// segment text outside the wrap continues in the surrounding color.
//
// Returns pctText unchanged when ThresholdTarget is anything other
// than "pct", when color is disabled, when no threshold matches, when
// the threshold FG equals the segment's static FG, or when fg256
// rejects the threshold FG value.
func wrapPct(pctText string, s Segment, p *payload, colorEnabled bool) string {
	if !colorEnabled || s.ThresholdTarget != "pct" {
		return pctText
	}
	fg := chooseFG(s, p)
	if fg == "" || fg == s.FG {
		return pctText
	}
	openInner := fg256(fg)
	if openInner == "" {
		return pctText
	}
	closeInner := "\x1b[39m"
	if reopen := fg256(s.FG); reopen != "" {
		closeInner = reopen
	}
	return openInner + pctText + closeInner
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestWrapPct' -v -count=1`

Expected: all five `TestWrapPct_*` tests PASS.

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): Segment.ThresholdTarget plus wrapPct helper

ThresholdTarget is a new optional Segment field with two meaningful
values: \"\" / \"all\" preserves the 0.1.10 behaviour (whole-segment
threshold override via the renderer's outer style() wrap), \"pct\"
signals that the segment will color only its percentage substring
internally. wrapPct is the helper that does the inner wrap and
closes back to the segment's static FG (or terminal default if the
segment has no static FG) so non-pct regions continue in the
surrounding color.

Nothing calls wrapPct yet — the next commits wire renderSegment,
renderContext, and renderLimit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `renderSegment` dispatch + regression test

**Files:**
- Modify: `internal/pkg/render/render.go` (`renderSegment`)
- Modify: `internal/pkg/render/render_test.go` (regression test for `"all"` default)

- [ ] **Step 1: Write the regression test**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_AllTargetStillWorksAsBefore(t *testing.T) {
	// With ThresholdTarget omitted (default "all"), the 0.1.10
	// behaviour holds — the outer style() call wraps the whole
	// segment in the threshold FG, including the bar and tokens.
	cfg := Config{Rows: [][]Segment{{
		{
			Type:  "context",
			Style: "bar+pct",
			FG:    "245",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}
	raw := []byte(`{"context_window":{"used_percentage":95,"context_window_size":1000000,"total_input_tokens":950000}}`)
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The whole segment must be wrapped in the threshold FG, so the
	// bar's opening bracket appears immediately after the opening
	// escape.
	if !strings.Contains(got, "\x1b[38;5;160m[") {
		t.Errorf("all-target should wrap the whole segment including the bar, got %q", got)
	}
	// The static FG (245) must not appear — chooseFG overrode it.
	if strings.Contains(got, "\x1b[38;5;245m") {
		t.Errorf("static fg 245 should be overridden by the threshold, got %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes pre-change**

Run: `go test ./internal/pkg/render -run 'TestRender_AllTargetStillWorksAsBefore' -v -count=1`

Expected: PASS. This test documents the 0.1.10 behaviour so the
upcoming dispatch tweak does not regress it.

- [ ] **Step 3: Update `renderSegment` to suppress outer override on "pct"**

In `internal/pkg/render/render.go`, the current `renderSegment` looks like:

```go
func renderSegment(p *payload, s Segment, env renderEnv) string {
	fn, ok := segmentFuncs[s.Type]
	if !ok {
		return "?" + s.Type + "?"
	}
	out := fn(p, s, env)
	if out == "" {
		return ""
	}
	return style(out, chooseFG(s, p), s.BG, s.Bold, env.colorEnabled)
}
```

Replace the final `return` statement so the outer wrap honours the static FG when the segment self-styles:

```go
func renderSegment(p *payload, s Segment, env renderEnv) string {
	fn, ok := segmentFuncs[s.Type]
	if !ok {
		return "?" + s.Type + "?"
	}
	out := fn(p, s, env)
	if out == "" {
		return ""
	}
	// For ThresholdTarget == "pct", the segment function has already
	// colored its percentage substring internally; the outer wrap
	// must therefore use the static FG so non-pct regions render in
	// the segment's neutral color, not in the threshold override.
	fg := s.FG
	if s.ThresholdTarget != "pct" {
		fg = chooseFG(s, p)
	}
	return style(out, fg, s.BG, s.Bold, env.colorEnabled)
}
```

- [ ] **Step 4: Run the test to verify it still passes**

Run: `go test ./internal/pkg/render -run 'TestRender_AllTargetStillWorksAsBefore' -v -count=1`

Expected: PASS — the dispatch tweak short-circuits only when
`ThresholdTarget == "pct"`, which is not the case here.

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): renderSegment skips chooseFG when ThresholdTarget=pct

When a segment self-styles its percentage substring, the renderer's
outer style() wrap must use the static FG, not the threshold
override — otherwise the non-pct text inherits the override too.
For ThresholdTarget=\"\" or \"all\" (the 0.1.10 default), behaviour
is unchanged. Regression test pins the 0.1.10 path.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `renderContext` routes its percentage through `wrapPct`

**Files:**
- Modify: `internal/pkg/render/segments.go` (`renderContext`)
- Modify: `internal/pkg/render/render_test.go` (one integration test)

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_ContextPctTargetColorsOnlyDigits(t *testing.T) {
	cfg := Config{Rows: [][]Segment{{
		{
			Type:            "context",
			Style:           "bar+pct",
			FG:              "245",
			ThresholdTarget: "pct",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}
	raw := []byte(`{"context_window":{"used_percentage":95,"context_window_size":1000000,"total_input_tokens":950000}}`)
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Only the "95%" substring must wrap in the threshold FG, closing
	// back to the static FG so the surrounding tokens stay neutral.
	if !strings.Contains(got, "\x1b[38;5;160m95%\x1b[38;5;245m") {
		t.Errorf("expected inner pct wrap '95%%' in fg 160 closing to fg 245, got %q", got)
	}
	// The bar must NOT start in the threshold FG.
	if strings.Contains(got, "\x1b[38;5;160m[") {
		t.Errorf("threshold FG should not wrap the bar in pct-target mode, got %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pkg/render -run 'TestRender_ContextPctTargetColorsOnlyDigits' -v -count=1`

Expected: FAIL — `renderContext` does not yet call `wrapPct`, so
the output has no inner wrap.

- [ ] **Step 3: Update `renderContext` to call `wrapPct`**

In `internal/pkg/render/segments.go`, the current `renderContext`
(lines 148-176) discards its `renderEnv` argument via `_`. It needs
to read `colorEnabled` from `env`, so rename the parameter to `env`
and route the percentage substring through `wrapPct`:

```go
// renderContext draws the context-window state. Style "bar+pct" (default)
// emits the unicode block bar, the rounded percent, and used/total token
// counts. "bar" or "pct" omit the other parts. Hidden when both
// UsedPercentage and ContextWindowSize are zero (no data). When
// Segment.ThresholdTarget=="pct" the percent digits are wrapped in the
// threshold FG via wrapPct so the bar and tokens stay in the static FG.
func renderContext(p *payload, s Segment, env renderEnv) string {
	if p.Context.UsedPercentage == 0 && p.Context.ContextWindowSize == 0 {
		return ""
	}
	style := s.Style
	if style == "" {
		style = "bar+pct"
	}
	pct := int(p.Context.UsedPercentage + 0.5) // round
	pctText := fmt.Sprintf("%d%%", pct)
	pctStyled := wrapPct(pctText, s, p, env.colorEnabled)
	bar := makeBar(p.Context.UsedPercentage, barCells)
	switch style {
	case "bar":
		return bar
	case "pct":
		return pctStyled
	default: // "bar+pct"
		// Use total_input_tokens for the visible "consumed" number — that's
		// the cumulative session input that actually drives the percentage.
		// current_usage.input_tokens is just the current turn's prompt size
		// after caching (often 1) and would render misleadingly as "0".
		used := p.Context.TotalInputTokens
		return fmt.Sprintf("%s %s %s/%s",
			bar, pctStyled, formatTokens(used), formatTokens(p.Context.ContextWindowSize))
	}
}
```

The `"bar"`-style path never emits the percent, so `wrapPct` is
harmless to compute but unused there — the call lifts out of the
switch for clarity even so.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pkg/render -run 'TestRender_Context' -v -count=1`

Expected: every `TestRender_Context*` test PASSes, including the
new `TestRender_ContextPctTargetColorsOnlyDigits`.

Run: `go test -race ./...`

Expected: every package green.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): renderContext routes its percentage through wrapPct

With ThresholdTarget=\"pct\", the percent digits get the threshold
FG via wrapPct while the bar cells and token counts continue in the
segment's static FG. The bar-only style path is unaffected; pct and
bar+pct route the formatted pct string through wrapPct before
embedding. Picks up env.colorEnabled by renaming the discarded
parameter.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `renderLimit` routes its percentage through `wrapPct`

**Files:**
- Modify: `internal/pkg/render/segments.go` (`renderLimit5h`, `renderLimit7d`, `renderLimit`)
- Modify: `internal/pkg/render/render_test.go` (one integration test)

- [ ] **Step 1: Write the failing test**

Append to `internal/pkg/render/render_test.go`:

```go
func TestRender_Limit5hPctTargetColorsOnlyDigits(t *testing.T) {
	// Pin the clock so the countdown is deterministic.
	const fixedNow = int64(1_000_000_000)
	prev := nowFunc
	nowFunc = func() time.Time { return time.Unix(fixedNow, 0) }
	defer func() { nowFunc = prev }()

	cfg := Config{Rows: [][]Segment{{
		{
			Type:            "limit_5h",
			FG:              "245",
			ThresholdTarget: "pct",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}
	raw := []byte(`{"rate_limits":{"five_hour":{"used_percentage":95,"resets_at":1000000300}}}`)
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// "95%" must be wrapped in the threshold FG, closing back to the static FG.
	if !strings.Contains(got, "\x1b[38;5;160m95%\x1b[38;5;245m") {
		t.Errorf("expected inner pct wrap '95%%' in fg 160 closing to fg 245, got %q", got)
	}
	// The "5h:" label must not start in the threshold FG.
	if strings.Contains(got, "\x1b[38;5;160m5h:") {
		t.Errorf("threshold FG should not wrap the label in pct-target mode, got %q", got)
	}
	// The countdown "(5m)" must not appear with the threshold FG.
	if strings.Contains(got, "\x1b[38;5;160m(") {
		t.Errorf("threshold FG should not wrap the countdown in pct-target mode, got %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pkg/render -run 'TestRender_Limit5hPctTargetColorsOnlyDigits' -v -count=1`

Expected: FAIL — `renderLimit` does not yet call `wrapPct` and does
not currently receive `p`.

- [ ] **Step 3: Extend `renderLimit` signature and call `wrapPct`**

In `internal/pkg/render/segments.go`, the current `renderLimit5h`,
`renderLimit7d`, and `renderLimit` look like this:

```go
func renderLimit5h(p *payload, s Segment, env renderEnv) string {
	return renderLimit(p.Limits.FiveHour, s, env, "5h")
}

func renderLimit7d(p *payload, s Segment, env renderEnv) string {
	return renderLimit(p.Limits.SevenDay, s, env, "7d")
}

func renderLimit(rl rateLimitF, s Segment, env renderEnv, defaultLabel string) string {
	if rl.UsedPercentage == 0 && rl.ResetsAt == 0 {
		return ""
	}
	label := s.Label
	if label == "" {
		label = defaultLabel
	}
	pct := formatPct(rl.UsedPercentage)

	switch s.Style {
	case "bar":
		return fmt.Sprintf("%s:%s", label, makeBar(rl.UsedPercentage, barCells))
	case "bar+pct":
		if rl.ResetsAt == 0 {
			return fmt.Sprintf("%s:%s %s", label, makeBar(rl.UsedPercentage, barCells), pct)
		}
		return fmt.Sprintf("%s:%s %s (%s)", label, makeBar(rl.UsedPercentage, barCells), pct, formatCountdown(rl.ResetsAt-env.nowUnix))
	default: // "" or "pct"
		if rl.ResetsAt == 0 {
			return fmt.Sprintf("%s:%s", label, pct)
		}
		return fmt.Sprintf("%s:%s (%s)", label, pct, formatCountdown(rl.ResetsAt-env.nowUnix))
	}
}
```

Replace the three functions with the versions below — `renderLimit`
takes an extra `p *payload` argument so it can call `wrapPct`:

```go
func renderLimit5h(p *payload, s Segment, env renderEnv) string {
	return renderLimit(p.Limits.FiveHour, p, s, env, "5h")
}

func renderLimit7d(p *payload, s Segment, env renderEnv) string {
	return renderLimit(p.Limits.SevenDay, p, s, env, "7d")
}

// renderLimit is the shared body of renderLimit5h and renderLimit7d.
// Hidden when both UsedPercentage and ResetsAt are zero. When
// Segment.ThresholdTarget=="pct" the percent digits are wrapped in
// the threshold FG via wrapPct so the label and countdown stay in
// the static FG.
//
// Style values:
//   - "" or "pct" (default): "<label>:<pct> (<countdown>)"
//   - "bar":                 "<label>:[bar]"
//   - "bar+pct":             "<label>:[bar] <pct> (<countdown>)"
func renderLimit(rl rateLimitF, p *payload, s Segment, env renderEnv, defaultLabel string) string {
	if rl.UsedPercentage == 0 && rl.ResetsAt == 0 {
		return ""
	}
	label := s.Label
	if label == "" {
		label = defaultLabel
	}
	pct := wrapPct(formatPct(rl.UsedPercentage), s, p, env.colorEnabled)

	switch s.Style {
	case "bar":
		return fmt.Sprintf("%s:%s", label, makeBar(rl.UsedPercentage, barCells))
	case "bar+pct":
		if rl.ResetsAt == 0 {
			return fmt.Sprintf("%s:%s %s", label, makeBar(rl.UsedPercentage, barCells), pct)
		}
		return fmt.Sprintf("%s:%s %s (%s)", label, makeBar(rl.UsedPercentage, barCells), pct, formatCountdown(rl.ResetsAt-env.nowUnix))
	default: // "" or "pct"
		if rl.ResetsAt == 0 {
			return fmt.Sprintf("%s:%s", label, pct)
		}
		return fmt.Sprintf("%s:%s (%s)", label, pct, formatCountdown(rl.ResetsAt-env.nowUnix))
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pkg/render -run 'TestRender_Limit5h' -v -count=1`

Expected: every `TestRender_Limit5h*` test PASSes, including the
new `TestRender_Limit5hPctTargetColorsOnlyDigits`.

Run: `go test -race ./...`

Expected: every package green. The golden-fixture suite
(`TestRender_GoldenFixtures`) must remain green — the existing
fixtures use no `threshold_target`, so the new branch is never
taken.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/render_test.go
git commit -s -m "feat(render): renderLimit routes its percentage through wrapPct

renderLimit gains a p *payload argument so it can call wrapPct on
the formatted percentage string. With ThresholdTarget=\"pct\", the
percent digits get the threshold FG while the label and countdown
stay in the segment's static FG. The two style-specific paths and
both ResetsAt branches all reuse the pre-wrapped pct value.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Relax the segmentFunc doc contract

**Files:**
- Modify: `internal/pkg/render/segments.go` (top-of-file comment block)

This is a documentation-only change. The behaviour was already
loosened by Tasks 3 and 4; the comment must follow.

- [ ] **Step 1: Update the doc comment**

In `internal/pkg/render/segments.go`, the current comment at the top
of the file (immediately above `type segmentFunc`) reads:

```go
// segmentFunc renders one segment. It MUST return "" to suppress the
// segment (the row joiner skips empty results), and MUST NOT return
// ANSI escape codes — colour wrapping happens in renderSegment via
// style().
type segmentFunc func(p *payload, s Segment, env renderEnv) string
```

Replace it with:

```go
// segmentFunc renders one segment. It MUST return "" to suppress the
// segment (the row joiner skips empty results). It MAY emit ANSI
// escape sequences for sub-region styling (e.g. threshold coloring
// of a percentage substring via wrapPct); when it does, it MUST
// close every opened sequence before returning so the renderer's
// outer style() wrap is not broken.
type segmentFunc func(p *payload, s Segment, env renderEnv) string
```

- [ ] **Step 2: Verify nothing else asserts the old wording**

Run: `grep -RIn 'MUST NOT return ANSI' .`

Expected: no matches anywhere in the tree (the old phrasing existed
only at this single site).

- [ ] **Step 3: Run the test suite**

Run: `go test -race ./...`

Expected: every package green. Pure comment edits cannot affect
runtime behaviour, but the run confirms `gofmt -l` would not
complain about the new comment block.

Run: `gofmt -l internal/pkg/render`

Expected: empty output.

- [ ] **Step 4: Commit**

```bash
git add internal/pkg/render/segments.go
git commit -s -m "docs(render): segments may emit ANSI for sub-region styling

The old contract forbade segment functions from returning ANSI
codes outright. wrapPct in 0.1.12 needs to wrap a substring in the
threshold FG, so the rule is relaxed: segments MAY emit ANSI
sequences, but MUST close every one before returning so the
renderer's outer style() wrap is not broken.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Documentation update

**Files:**
- Modify: `docs/configuration.md`

- [ ] **Step 1: Add `threshold_target` to the type-specific fields table**

The current table sits under `### Type-specific fields`. Find this
row in `docs/configuration.md`:

```markdown
| `thresholds`    | `context`, `limit_5h`, `limit_7d`    | Per-segment percentage-driven `fg` overrides. See [Thresholds](#thresholds). |
```

Add a new row immediately below it:

```markdown
| `threshold_target` | `context`, `limit_5h`, `limit_7d` | `"all"` (default) wraps the whole segment in the threshold `fg`; `"pct"` wraps only the percentage digits. See [Thresholds](#thresholds). |
```

- [ ] **Step 2: Expand the Thresholds subsection**

The current `### Thresholds` subsection ends with this paragraph:

```markdown
Threshold ordering inside the array does not matter; the renderer picks
by `min`, not by position. `bg` and `bold` are not part of the
threshold schema — pick those statically on the segment.
```

Append the following paragraphs immediately after it:

```markdown
By default, when a threshold matches, its `fg` is applied to the
entire segment via the renderer's outer style wrap. That includes
the bar cells, the percentage digits, the token counts, and the
label — every glyph in the segment switches color together. For
narrow displays (single line, modest contrast) this is the desired
behaviour; for wider layouts the full-segment color shift can
overwhelm.

The optional `threshold_target` field scopes the override:

- `"all"` (default, omitted): the existing whole-segment behaviour.
- `"pct"`: only the percentage digits switch to the threshold `fg`.
  The bar cells, token counts, and label keep the segment's static
  `fg`.

```json
{"type": "context", "style": "bar+pct", "fg": "245",
 "threshold_target": "pct",
 "thresholds": [
   {"min": 70, "fg": "136"},
   {"min": 90, "fg": "160"}
 ]}
```

In this configuration the bar `[██░░░░░░░░░░░░░░]` stays in `245`
(grey) regardless of fill; only the `95%` flicks to `160` (red)
above the 90 threshold.
```

- [ ] **Step 3: Cross-reference from the per-segment entries**

The current `#### `context`` entry contains this line:

```markdown
Token counts are compacted: `1234` → `"1k"`, `1_500_000` → `"1.5M"`. Hidden
when both `used_percentage` and `context_window_size` are zero. Supports
[`thresholds`](#thresholds) to switch `fg` based on `used_percentage`.
```

Replace it with:

```markdown
Token counts are compacted: `1234` → `"1k"`, `1_500_000` → `"1.5M"`. Hidden
when both `used_percentage` and `context_window_size` are zero. Supports
[`thresholds`](#thresholds) (whole-segment or `threshold_target: "pct"`
for just the percentage digits) to switch `fg` based on
`used_percentage`.
```

The current `#### `limit_5h`, `limit_7d`` entry contains:

```markdown
and `resets_at` are zero (no data). Both segments support
[`thresholds`](#thresholds) to switch `fg` based on `used_percentage`.
```

Replace it with:

```markdown
and `resets_at` are zero (no data). Both segments support
[`thresholds`](#thresholds) (whole-segment or `threshold_target: "pct"`
for just the percentage digits) to switch `fg` based on
`used_percentage`.
```

- [ ] **Step 4: Verify the file still parses as valid markdown**

Run: `git diff docs/configuration.md`

Expected: only the four insertions / replacements above; nothing else
touched.

- [ ] **Step 5: Commit**

```bash
git add docs/configuration.md
git commit -s -m "docs(render): document Segment.ThresholdTarget

Adds threshold_target to the per-segment field table, expands the
Thresholds subsection to cover the new \"all\" (default) versus
\"pct\" semantics with a worked example, and cross-references the
field from the context, limit_5h, and limit_7d entries.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

After Task 6, the branch is feature-complete. Before squashing into
`development-0.1.12-main`:

- [ ] Run the full suite once more: `go test -race -cover ./...` — every package PASSes.
- [ ] Lint: `go vet ./...` produces no output; `gofmt -l .` is empty.
- [ ] Manual smoke against the user's payload corpus:

```bash
go build -o bin/ccsb ./cmd/ccsb
# Take a recent real capture and override threshold_target via a temp config.
TMP_CFG=$(mktemp -d)
mkdir -p "$TMP_CFG/ccsb"
cat > "$TMP_CFG/ccsb/config.json" <<'EOF'
{
  "render": {
    "rows": [
      [
        {"type": "context", "style": "bar+pct", "fg": "245",
         "threshold_target": "pct",
         "thresholds": [{"min": 70, "fg": "136"}, {"min": 90, "fg": "160"}]}
      ]
    ]
  }
}
EOF
latest=$(ls -1t ~/.local/state/ccsb/captures/*.json | head -1)
XDG_CONFIG_HOME=$TMP_CFG ./bin/ccsb < "$latest"
```

Expected at 95 %: bar stays grey (no `\x1b[38;5;160m[`), `95%` is in
red (`\x1b[38;5;160m95%\x1b[38;5;245m`).

Expected at 50 %: no escape codes for the threshold colors (only the
static `\x1b[38;5;245m` wrap from the outer `style()`).

Expected with `--no-color` semantics (`NO_COLOR=1`): the bar and
percent both render in plain text — no ANSI anywhere.

## Out of scope (reminder)

The spec explicitly excludes these — do not add them:

- Sub-region targeting other than the percentage digits.
- Background-color thresholds (`bg`).
- A `Threshold.Bold` field.
- Sub-region thresholds for non-percentage segments (`cost`, `lines`).
- Powerline separators with fg/bg transitions, and terminal-width-aware
  layout — both Phase 3 / 0.2.0 territory; the design memo lives at
  `~/.claude/projects/.../memory/02x_terminal_aware_layout.md`.

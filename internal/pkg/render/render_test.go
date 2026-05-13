package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func TestRender_EmptyConfigUsesDefaultRows(t *testing.T) {
	raw := []byte(`{"model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// default layout produces 3 rows
	if n := strings.Count(got, "\n"); n < 2 {
		t.Errorf("want >=2 newlines, got %d in %q", n, got)
	}
	// model segment is now registered, so expect the display name
	if !strings.Contains(got, "Opus 4.7") {
		t.Errorf("want Opus 4.7 in output, got %q", got)
	}
}

func TestRender_GlobalParseFailureReturnsLastResort(t *testing.T) {
	got, err := Render(Options{}, []byte("not json"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got == "" {
		t.Error("must emit something even on parse failure")
	}
}

func TestRender_UnknownSegmentTypeRendersMarker(t *testing.T) {
	cfg := Config{Rows: []Row{{Segments: []Segment{{Type: "frobnicate"}}}}}
	raw := []byte(`{"model":{"display_name":"Opus"}}`)
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "?frobnicate?") {
		t.Errorf("want ?frobnicate? marker, got %q", got)
	}
}

func TestRender_RowsJoinedWithNewline_SegmentsWithSeparator(t *testing.T) {
	cfg := Config{
		Rows: []Row{
			{Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}}},
			{Segments: []Segment{{Type: "text", Label: "C"}}},
		},
		Separator: " | ",
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "A | B\nC"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRender_DefaultSeparator(t *testing.T) {
	cfg := Config{Rows: []Row{{Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}}}}}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if got != "A | B" {
		t.Errorf("default separator should be ' | ', got %q", got)
	}
}

func TestRender_DefaultLayoutAgainstSamplePayload(t *testing.T) {
	raw := []byte(`{
		"model": {"display_name": "Opus 4.7 (1M context)"},
		"workspace": {"current_dir": "/home/u/projects/foo"},
		"exceeds_200k_tokens": true,
		"cost": {"total_cost_usd": 17.7024},
		"context_window": {
			"used_percentage": 27,
			"context_window_size": 1000000,
			"total_input_tokens": 273000
		},
		"rate_limits": {
			"five_hour":  {"used_percentage": 7.0,  "resets_at": 200},
			"seven_day":  {"used_percentage": 30,   "resets_at": 700}
		}
	}`)
	got, err := Render(Options{Cwd: "/tmp"}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Strip ANSI codes for assertion (color codes may be present)
	cleanGot := stripANSI(got)
	if !strings.HasPrefix(cleanGot, "Opus 4.7 1M | [████░░░░░░░░░░░░] 27% 273k/1M\n") {
		t.Errorf("first row mismatch:\n%s", cleanGot)
	}
	if !strings.Contains(cleanGot, "$17.70") || !strings.Contains(cleanGot, "5h: 7%") {
		t.Errorf("second row missing cost or 5h rate:\n%s", cleanGot)
	}
	// Third row should contain the cwd basename. git_branch may or may not
	// fire depending on the test runner's cwd; just assert "foo" is present.
	if !strings.Contains(got, "foo") {
		t.Errorf("last row should include cwd basename:\n%s", got)
	}
}

var updateGolden = flag.Bool("update", false, "update golden files in testdata/golden/")

func TestRender_GoldenFixtures(t *testing.T) {
	// Pin the clock so countdown formatting is deterministic.
	const fixedNow = int64(1_778_400_000) // a few thousand seconds before the resets_at values in fixtures
	prev := nowFunc
	nowFunc = func() time.Time { return time.Unix(fixedNow, 0) }
	defer func() { nowFunc = prev }()

	cases := []string{
		"low_cost",
		"high_cost_1m",
		"near_5h_limit",
		"after_5h_reset",
		"detached_head",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			payloadPath := filepath.Join("testdata", "payloads", name+".json")
			goldenPath := filepath.Join("testdata", "golden", name+".txt")

			raw, err := os.ReadFile(payloadPath)
			if err != nil {
				t.Fatalf("read payload: %v", err)
			}
			got, err := Render(Options{NoColor: true}, raw)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if *updateGolden {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Errorf("%s mismatch:\n got: %q\nwant: %q", name, got, string(want))
			}
		})
	}
}

func TestChooseFG_NoThresholdsReturnsStaticFG(t *testing.T) {
	s := Segment{Type: "context", FG: "75"}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	if got := chooseFG(s, p); got != "75" {
		t.Errorf("got %q, want %q", got, "75")
	}
}

func TestChooseFG_HighestMatchingMinWins(t *testing.T) {
	s := Segment{
		Type: "context",
		FG:   "245",
		Thresholds: []Threshold{
			{Min: 70, FG: "136"},
			{Min: 90, FG: "160"},
		},
	}
	// 75% → only min=70 matches → 136.
	p := &payload{Context: contextF{UsedPercentage: 75}}
	if got := chooseFG(s, p); got != "136" {
		t.Errorf("at 75: got %q, want %q", got, "136")
	}
	// 92% → both match; higher-min (90) wins → 160.
	p.Context.UsedPercentage = 92
	if got := chooseFG(s, p); got != "160" {
		t.Errorf("at 92: got %q, want %q", got, "160")
	}
	// Threshold-list order must not matter; reverse and rerun.
	s.Thresholds = []Threshold{
		{Min: 90, FG: "160"},
		{Min: 70, FG: "136"},
	}
	if got := chooseFG(s, p); got != "160" {
		t.Errorf("reversed order at 92: got %q, want %q", got, "160")
	}
}

func TestChooseFG_BelowAllThresholdsUsesStaticFG(t *testing.T) {
	s := Segment{
		Type: "context",
		FG:   "245",
		Thresholds: []Threshold{
			{Min: 70, FG: "136"},
			{Min: 90, FG: "160"},
		},
	}
	p := &payload{Context: contextF{UsedPercentage: 25}}
	if got := chooseFG(s, p); got != "245" {
		t.Errorf("got %q, want %q", got, "245")
	}
}

func TestChooseFG_EmptyFGThresholdIsSkipped(t *testing.T) {
	s := Segment{
		Type: "context",
		FG:   "245",
		Thresholds: []Threshold{
			{Min: 70, FG: ""}, // skipped — no useful override
			{Min: 90, FG: "160"},
		},
	}
	// 75% would match the 70-threshold, but its FG is empty → fall through.
	p := &payload{Context: contextF{UsedPercentage: 75}}
	if got := chooseFG(s, p); got != "245" {
		t.Errorf("at 75 with empty FG threshold: got %q, want %q", got, "245")
	}
	// 95% still matches the 90-threshold normally.
	p.Context.UsedPercentage = 95
	if got := chooseFG(s, p); got != "160" {
		t.Errorf("at 95: got %q, want %q", got, "160")
	}
}

func TestChooseFG_NonMetricSegmentIgnoresThresholds(t *testing.T) {
	// model has no percentage metric → thresholds must not change anything.
	s := Segment{
		Type:       "model",
		FG:         "75",
		Thresholds: []Threshold{{Min: 0, FG: "160"}},
	}
	p := &payload{Context: contextF{UsedPercentage: 95}}
	if got := chooseFG(s, p); got != "75" {
		t.Errorf("got %q, want %q", got, "75")
	}
}

func TestChooseFG_Limit5hAndLimit7dUseTheirOwnPercentage(t *testing.T) {
	p := &payload{Limits: limitsF{
		FiveHour: rateLimitF{UsedPercentage: 95},
		SevenDay: rateLimitF{UsedPercentage: 20},
	}}
	s5h := Segment{Type: "limit_5h", FG: "245",
		Thresholds: []Threshold{{Min: 90, FG: "160"}}}
	if got := chooseFG(s5h, p); got != "160" {
		t.Errorf("limit_5h at 95: got %q, want %q", got, "160")
	}
	s7d := Segment{Type: "limit_7d", FG: "245",
		Thresholds: []Threshold{{Min: 90, FG: "160"}}}
	if got := chooseFG(s7d, p); got != "245" {
		t.Errorf("limit_7d at 20: got %q, want %q", got, "245")
	}
}

func TestRender_ContextThresholdProducesExpectedAnsi(t *testing.T) {
	cfg := Config{Rows: []Row{{Segments: []Segment{
		{Type: "context", Style: "pct", FG: "245",
			Thresholds: []Threshold{
				{Min: 70, FG: "136"},
				{Min: 90, FG: "160"},
			}},
	}}}}
	raw := []byte(`{"context_window":{"used_percentage":95,"context_window_size":1000000,"total_input_tokens":950000}}`)
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "\x1b[38;5;160m") {
		t.Errorf("expected \\x1b[38;5;160m (fg 160) in output, got %q", got)
	}
	if strings.Contains(got, "\x1b[38;5;245m") {
		t.Errorf("static fg 245 should be overridden, but appears in output %q", got)
	}
}

func TestRender_StyledEmptySegmentEmitsNothing(t *testing.T) {
	// A git_branch segment outside a git repo returns "" from its renderer.
	// Even with FG/Bold set, the row must not emit any ANSI escapes —
	// styling an empty string just produces dead bytes.
	cfg := Config{Rows: []Row{{Segments: []Segment{
		{Type: "git_branch", FG: "33", Bold: true},
	}}}}
	got, err := Render(Options{Config: cfg, Cwd: t.TempDir()}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("styled-empty segment leaked ANSI: %q", got)
	}
	if got != "" {
		t.Errorf("row with only an empty segment must be empty, got %q", got)
	}
}

func TestRender_StyledNonEmptySegmentStillWraps(t *testing.T) {
	// Regression: the empty-text early-return must not affect normal segments.
	cfg := Config{Rows: []Row{{Segments: []Segment{
		{Type: "text", Label: "hello", FG: "33"},
	}}}}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "\x1b[38;5;33mhello\x1b[0m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

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

func TestWrapPct_InvalidThresholdFGIsNoOp(t *testing.T) {
	// chooseFG does not validate threshold FG values; a malformed
	// string (here "999", which fg256 rejects because 999 > 255)
	// must short-circuit wrapPct back to the raw pct text rather
	// than emitting a broken escape sequence.
	s := Segment{
		Type:            "context",
		FG:              "245",
		ThresholdTarget: "pct",
		Thresholds:      []Threshold{{Min: 0, FG: "999"}},
	}
	p := &payload{Context: contextF{UsedPercentage: 50}}
	if got := wrapPct("50%", s, p, true); got != "50%" {
		t.Errorf("got %q, want raw text", got)
	}
}

func TestRender_ContextPctTargetColorsOnlyDigits(t *testing.T) {
	cfg := Config{Rows: []Row{{Segments: []Segment{
		{
			Type:            "context",
			Style:           "bar+pct",
			FG:              "245",
			ThresholdTarget: "pct",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}}
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

func TestRender_AllTargetStillWorksAsBefore(t *testing.T) {
	// With ThresholdTarget omitted (default "all"), the 0.1.10
	// behaviour holds — the outer style() call wraps the whole
	// segment in the threshold FG, including the bar and tokens.
	cfg := Config{Rows: []Row{{Segments: []Segment{
		{
			Type:  "context",
			Style: "bar+pct",
			FG:    "245",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}}
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
	// [████░░] = '[' + 4×U+2588 FULL BLOCK + 2×U+2591 LIGHT SHADE + ']'
	// = 8 characters, all width 1.
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

func TestRender_Limit5hPctTargetColorsOnlyDigits(t *testing.T) {
	// Pin the clock so the countdown is deterministic.
	const fixedNow = int64(1_000_000_000)
	prev := nowFunc
	nowFunc = func() time.Time { return time.Unix(fixedNow, 0) }
	defer func() { nowFunc = prev }()

	cfg := Config{Rows: []Row{{Segments: []Segment{
		{
			Type:            "limit_5h",
			FG:              "245",
			ThresholdTarget: "pct",
			Thresholds: []Threshold{
				{Min: 90, FG: "160"},
			},
		},
	}}}}
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
		if len(row.Segments) != 1 {
			t.Errorf("row %d: got %d segments, want 1", i, len(row.Segments))
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
	if n := strings.Count(got, powerlineChevron); n != 2 {
		t.Errorf("chevron count: got %d, want 2 in %q", n, got)
	}
	// Chevron carries the muted-grey fg.
	if !strings.Contains(got, "\x1b[38;5;245m") {
		t.Errorf("chevron should be wrapped in fg 245, got %q", got)
	}
}

func TestRenderRowPowerline_EmptySegmentDropsChevron(t *testing.T) {
	// Three segments where the middle renders empty → only one chevron.
	row := Row{Bg: "234", Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "git_branch"}, // renders "" (no payload data)
		{Type: "text", Label: "C"},
	}}
	env := renderEnv{colorEnabled: true, ttyCols: 0}
	got := renderRowPowerline(&payload{}, row, env)
	if n := strings.Count(got, powerlineChevron); n != 1 {
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
	// A row with empty Bg still renders but never emits \x1b[48;...
	row := Row{Bg: "", Segments: []Segment{{Type: "text", Label: "A"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 80}
	got := renderRowPowerline(&payload{}, row, env)
	if strings.Contains(got, "\x1b[48;") {
		t.Errorf("empty Bg should not emit bg escape, got %q", got)
	}
}

func TestRenderRowPowerline_NoColorReturnsEmpty(t *testing.T) {
	// Powerline emits ANSI unconditionally; the function must return
	// "" when called with colorEnabled false so a future caller cannot
	// accidentally inject escapes into a no-color stream.
	row := Row{Bg: "234", Segments: []Segment{{Type: "text", Label: "A"}}}
	env := renderEnv{colorEnabled: false, ttyCols: 80}
	if got := renderRowPowerline(&payload{}, row, env); got != "" {
		t.Errorf("colorEnabled=false: got %q, want \"\"", got)
	}
}

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
	if strings.Contains(got, powerlineChevron) {
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
	if !strings.Contains(got, powerlineChevron) {
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
	if strings.Contains(got, powerlineChevron) {
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

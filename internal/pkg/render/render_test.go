package render

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func TestRender_EmptyConfigUsesDefaultConfig(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// default layout is two rows.
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("want exactly 1 newline (2 rows), got %d in %q", n, got)
	}
	if !strings.Contains(got, "Opus 4.7") {
		t.Errorf("want Opus 4.7 in output, got %q", got)
	}
	// Default config enables Powerline + round caps — first row must
	// open with the U+E0B6 left-cap glyph in fg=234 (palette[0]).
	if !strings.Contains(got, "\x1b[38;5;232m") {
		t.Errorf("default config must produce Powerline left-cap (fg=232 = globalPalette[0]), got %q", got)
	}
}

func TestRender_DefaultLayoutShowsVersionWhenSet(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{NoColor: true, Version: "0.2.7"}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("want exactly 1 newline (2 rows), got %d in %q", n, got)
	}
	if !strings.Contains(got, "v0.2.7") {
		t.Errorf("version must appear in last row, got %q", got)
	}
}

func TestRender_DefaultLayoutDevVersionShowsSkull(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{NoColor: true, Version: "dev"}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "☠ vdev") {
		t.Errorf("dev version must render with skull prefix, got %q", got)
	}
}

func TestDefaultConfig_VersionSegmentHasUpdateCheckDefaults(t *testing.T) {
	var version Segment
	found := false
	for _, row := range defaultConfig.Rows {
		for _, s := range row.Segments {
			if s.Type == "version" {
				version = s
				found = true
			}
		}
	}
	if !found {
		t.Fatal("default config has no version segment")
	}
	if !version.CheckUpdate {
		t.Error("default version segment must have check_update enabled")
	}
	for name, got := range map[string]string{
		"UpdateMinorFG": version.UpdateMinorFG,
		"UpdateMajorFG": version.UpdateMajorFG,
		"UpdateBigFG":   version.UpdateBigFG,
	} {
		if got == "" {
			t.Errorf("default version segment: %s is empty, want a color", name)
		}
	}
}

// TestRender_BrokenPayloadRendersSchemaHealthMarker pins the
// never-blank guarantee of the default layout: for any payload the
// parser cannot make sense of, detectSchemaIssue fires and the default
// row's schema_health segment emits the "☠" marker, so Claude Code
// always gets a non-empty status line.
func TestRender_BrokenPayloadRendersSchemaHealthMarker(t *testing.T) {
	for _, raw := range []string{"not json", "{}", "[1,2,3]", `{"model":123}`} {
		got, err := Render(Options{}, []byte(raw))
		if err != nil {
			t.Fatalf("Render(%q): %v", raw, err)
		}
		if !strings.Contains(got, "☠") {
			t.Errorf("Render(%q) = %q, want the schema_health ☠ marker", raw, got)
		}
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
		Margin: new(0),
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
	cfg := Config{
		Margin: new(0),
		Rows:   []Row{{Segments: []Segment{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if got != "A | B" {
		t.Errorf("default separator should be ' | ', got %q", got)
	}
}

func TestRender_DefaultLayoutAgainstSamplePayload(t *testing.T) {
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus 4.7 (1M context)"},
		"workspace": {"current_dir": "/home/u/projects/foo"},
		"exceeds_200k_tokens": true,
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
	got, err := Render(Options{Cwd: "/tmp", NoColor: true, Config: Config{Margin: new(0)}}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Two rows. Row 1 carries model + context + 5h + 7d; row 2
	// carries the cwd basename. NoColor degrades Powerline to the
	// natural separator path.
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d:\n%s", len(rows), got)
	}
	// Row 1 substrings — 5h/7d now render as "bar+pct" in defaultConfig,
	// so "5h: 7%" no longer appears as a contiguous substring; check the
	// pieces separately.
	for _, want := range []string{"Opus 4.7 1M", "27% 273k/1M", "5h:", " 7%", "7d:", " 30%"} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("row 1 missing %q:\n%s", want, rows[0])
		}
	}
	// Schema health must NOT fire on this valid payload.
	if strings.Contains(rows[0], "☠") {
		t.Errorf("schema_health should be hidden on a valid payload:\n%s", rows[0])
	}
	if !strings.Contains(rows[1], "foo") {
		t.Errorf("row 2 should include cwd basename:\n%s", rows[1])
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
			// Pin the terminal width so the golden fixtures stay
			// reflow-agnostic: a developer running tests inside an 80-col
			// pane would otherwise trip rowOverflows and split row 1 in
			// two. 200 cols comfortably covers every fixture's row 1.
			got, err := Render(Options{NoColor: true, Config: Config{Width: 200}}, raw)
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
	cfg := Config{
		Margin: new(0),
		Rows:   []Row{{Segments: []Segment{{Type: "text", Label: "hello", FG: "33"}}}},
	}
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
	if !strings.Contains(got, "\x1b[38;5;160m●") {
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

func TestDisplayWidth_CircleBar(t *testing.T) {
	// ●●◑○○○ = 3×U+25CF + 1×U+25D1 + 2×U+25CB = 6 chars, all width 1.
	if got := displayWidth("●●◑○○○"); got != 6 {
		t.Errorf("displayWidth circle bar: got %d, want 6", got)
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
	// The countdown "· 5m" must not start with the threshold FG.
	if strings.Contains(got, "\x1b[38;5;160m·") {
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
	if n := strings.Count(got, powerlineThinGlyph); n != 2 {
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
	if n := strings.Count(got, powerlineThinGlyph); n != 1 {
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

func TestRenderRowPowerline_NoBgUsesGlobalPalette(t *testing.T) {
	// A row with no Bg and no Palette falls back to env.globalPalette in
	// Powerline mode. The first visible segment picks globalPalette[0]
	// (here "232" — the dark end of the default 9-grey palette), so a bg
	// escape MUST appear.
	row := Row{Bg: "", Segments: []Segment{{Type: "text", Label: "A"}}}
	env := renderEnv{colorEnabled: true, ttyCols: 80, globalPalette: defaultGlobalPalette}
	got := renderRowPowerline(&payload{}, row, env)
	if !strings.Contains(got, "\x1b[48;5;232m") {
		t.Errorf("no-Bg row must use globalPalette[0] = 232, got %q", got)
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
	want := " " + powerlineThinGlyph + " "
	if n := strings.Count(stripped, want); n != 2 {
		t.Errorf("expected %q to appear 2 times in stripped output, got %d\nstripped: %q", want, n, stripped)
	}
}

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
	if strings.Contains(got, powerlineThinGlyph) {
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
	if !strings.Contains(got, powerlineThinGlyph) {
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
	if strings.Contains(got, powerlineThinGlyph) {
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
	// Swap devTTYWinsizeReader to a deterministic fake; verify the
	// resulting row reaches exactly ttyCols visible columns when margin
	// is zero (so bg-fill spans the full width).
	prev := devTTYWinsizeReader
	defer func() { devTTYWinsizeReader = prev }()
	devTTYWinsizeReader = func() (int, int, bool) { return 40, 24, true }

	cfg := Config{
		Powerline: true,
		Margin:    new(0), // disable margin so bg-fill == ttyCols
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

func TestRender_PopulatesTTYColsAndRowsViaDiscover(t *testing.T) {
	// Stub the three reader vars so detection returns a deterministic
	// (cols, rows). The tty_size segment surfaces those values to the
	// output so the test can check the integration end-to-end.
	prevDev, prevStat, prevFD := devTTYWinsizeReader, procStatReader, procFDWinsizeReader
	defer func() {
		devTTYWinsizeReader = prevDev
		procStatReader = prevStat
		procFDWinsizeReader = prevFD
	}()
	devTTYWinsizeReader = func() (int, int, bool) { return 96, 24, true }
	procStatReader = func(pid int) ([]byte, error) {
		t.Fatal("procStatReader must not be called when /dev/tty succeeds")
		return nil, nil
	}
	procFDWinsizeReader = func(path string) (int, int, error) {
		t.Fatal("procFDWinsizeReader must not be called when /dev/tty succeeds")
		return 0, 0, nil
	}

	cfg := Config{
		Rows: []Row{{Segments: []Segment{{Type: "tty_size"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "96×24") {
		t.Errorf("expected output to contain %q, got %q", "96×24", got)
	}
}

func TestConfigJSON_WidthRoundTrip(t *testing.T) {
	// Config.Width must marshal as the "width" JSON key, omitempty
	// when zero, and unmarshal back to the same int.
	zero := Config{}
	out, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(out), `"width"`) {
		t.Errorf("zero Config must omit width, got %s", out)
	}

	set := Config{Width: 200}
	out, err = json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	if !strings.Contains(string(out), `"width":200`) {
		t.Errorf("Config{Width:200} must encode width, got %s", out)
	}

	var back Config
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Width != 200 {
		t.Errorf("round-trip width: got %d, want 200", back.Width)
	}
}

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

func TestEffectiveSegmentBg(t *testing.T) {
	cases := []struct {
		name          string
		row           Row
		seg           Segment
		visibleIndex  int
		globalPalette []string
		paletteStart  int
		want          string
	}{
		{
			name: "Segment.BG overrides everything",
			row:  Row{Bg: "100", Palette: []string{"234"}},
			seg:  Segment{BG: "200"},
			want: "200",
		},
		{
			name:         "Row.Palette rotates by visibleIndex",
			row:          Row{Palette: []string{"234", "236", "238"}},
			seg:          Segment{},
			visibleIndex: 4,
			want:         "236", // 4 % 3 == 1
		},
		{
			name:          "Row.Palette beats globalPalette",
			row:           Row{Palette: []string{"100"}},
			seg:           Segment{},
			visibleIndex:  0,
			globalPalette: []string{"232", "233", "234"},
			paletteStart:  2,
			want:          "100",
		},
		{
			name: "Row.Bg used when no Palette and no Segment.BG",
			row:  Row{Bg: "100"},
			seg:  Segment{},
			want: "100",
		},
		{
			name:          "globalPalette when no row source",
			row:           Row{},
			seg:           Segment{},
			visibleIndex:  1,
			globalPalette: []string{"232", "233", "234", "235"},
			paletteStart:  0,
			want:          "233", // (0+1) % 4
		},
		{
			name:          "globalPalette honors paletteStart offset",
			row:           Row{},
			seg:           Segment{},
			visibleIndex:  0,
			globalPalette: []string{"232", "233", "234", "235", "236"},
			paletteStart:  2,
			want:          "234", // (2+0) % 5
		},
		{
			name: "empty string when globalPalette empty",
			row:  Row{},
			seg:  Segment{},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveSegmentBg(c.row, c.seg, c.visibleIndex, c.globalPalette, c.paletteStart)
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
		if got := effectiveSegmentBg(row, Segment{}, i, nil, 0); got != w {
			t.Errorf("visibleIndex=%d: got %q, want %q", i, got, w)
		}
	}
}

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
		Margin: new(0),
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

	// Find the chevron glyph in the raw output and grab everything
	// before it. The pre-space MUST be preceded by bg256(prevBg="234").
	// If the output instead contains bg256(nextBg="236") immediately
	// before the pre-space, the 0.2.3 bug has regressed.
	before, _, found := strings.Cut(got, powerlineThinGlyph)
	if !found {
		t.Fatalf("chevron glyph not in output: %q", got)
	}
	preSpaceBg234 := "\x1b[48;5;234m \x1b[48;5;234m"
	preSpaceBg236 := "\x1b[48;5;236m \x1b[48;5;236m"
	if !strings.Contains(before, preSpaceBg234) {
		t.Errorf("pre-chevron region must include bg=234 (prev) space, got prefix %q", before)
	}
	if strings.Contains(before, preSpaceBg236) {
		t.Errorf("pre-chevron region must NOT have bg=236 (next) before glyph — regression of 0.2.3 bug; got prefix %q", before)
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

func TestRenderRowPowerline_GlobalPaletteUsedWhenNoRowSource(t *testing.T) {
	row := Row{
		Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B"},
			{Type: "text", Label: "C"},
		},
	}
	env := renderEnv{colorEnabled: true, globalPalette: defaultGlobalPalette}
	got := renderRowPowerline(&payload{}, row, env)
	// paletteStart=0, stride=1 (default within this row): segments map
	// to globalPalette[0..2] = "232", "233", "234".
	for _, want := range []string{"\x1b[48;5;232m", "\x1b[48;5;233m", "\x1b[48;5;234m"} {
		if !strings.Contains(got, want) {
			t.Errorf("global-palette bg %q missing\nfull: %q", want, got)
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
		Caps:    true,
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
		t.Errorf("padded visible width with caps: got %d, want 78 (= margin(2) + cap(1) + content(9) + pad(65) + cap(1))\noutput: %q", w, got)
	}
}

func TestRenderRowRight_NoTTYColsDegradesToMarginPlusContent(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "v0.2.6"}}}
	env := renderEnv{colorEnabled: false, margin: 2, ttyCols: 0}
	got := renderRowRight(&payload{}, row, env, " | ")
	if got != "  v0.2.6" {
		t.Errorf("got %q, want %q", got, "  v0.2.6")
	}
}

func TestRenderRowRight_PadsToRightEdge(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "AB"}}}
	env := renderEnv{colorEnabled: false, margin: 2, ttyCols: 20}
	got := renderRowRight(&payload{}, row, env, " | ")
	// usable = 20 - 4 = 16; content width = 2; pad = 14
	// result = "  " + 14 spaces + "AB" = 18 chars visible
	if w := displayWidth(got); w != 18 {
		t.Errorf("display width: got %d, want 18 in %q", w, got)
	}
	if !strings.HasSuffix(got, "AB") {
		t.Errorf("content must be at right edge, got %q", got)
	}
}

func TestRenderRowRight_ZeroMargin(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "X"}}}
	env := renderEnv{colorEnabled: false, margin: 0, ttyCols: 10}
	got := renderRowRight(&payload{}, row, env, " | ")
	// usable = 10; pad = 9; total = 10
	if w := displayWidth(got); w != 10 {
		t.Errorf("display width: got %d, want 10 in %q", w, got)
	}
	if !strings.HasSuffix(got, "X") {
		t.Errorf("content at right edge, got %q", got)
	}
}

func TestRenderRowRight_ContentWiderThanUsable_NoCrash(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "ABCDEFGHIJ"}}}
	env := renderEnv{colorEnabled: false, margin: 2, ttyCols: 8}
	got := renderRowRight(&payload{}, row, env, " | ")
	// usable = 4; content = 10 > usable → pad = 0; output = "  " + content
	if got != "  ABCDEFGHIJ" {
		t.Errorf("got %q, want %q", got, "  ABCDEFGHIJ")
	}
}

func TestRenderRowRight_AllEmptySegmentsReturnsEmpty(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "git_branch"}}}
	env := renderEnv{colorEnabled: false, margin: 2, ttyCols: 80}
	if got := renderRowRight(&payload{}, row, env, " | "); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestRender_AlignRightBypassesPowerline(t *testing.T) {
	// Even with Powerline=true, a row with Align="right" must not emit
	// row-bg or chevron glyphs.
	cfg := Config{
		Powerline: true,
		Margin:    new(0),
		Rows: []Row{
			{
				Align:    "right",
				Bg:       "234",
				Segments: []Segment{{Type: "text", Label: "vX"}},
			},
		},
	}
	prevDev := devTTYWinsizeReader
	defer func() { devTTYWinsizeReader = prevDev }()
	devTTYWinsizeReader = func() (int, int, bool) { return 20, 24, true }

	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasSuffix(got, "vX") {
		t.Errorf("right-aligned row must end with content, got %q", got)
	}
	if strings.Contains(got, powerlineThinGlyph) || strings.Contains(got, "\x1b[48;5;234m") {
		t.Errorf("right-aligned row must bypass Powerline, got %q", got)
	}
}

func TestRender_VersionSegmentRendersVersion(t *testing.T) {
	cfg := Config{
		Margin: new(0),
		Rows:   []Row{{Segments: []Segment{{Type: "version"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true, Version: "1.2.3"}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", got)
	}
}

func TestRender_VersionSegmentHiddenWhenVersionEmpty(t *testing.T) {
	cfg := Config{
		Margin: new(0),
		Rows:   []Row{{Segments: []Segment{{Type: "version"}}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true, Version: ""}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "" {
		t.Errorf("version segment must be hidden when Version is empty, got %q", got)
	}
}

func TestRenderRowPowerline_LeftCapWithoutBgSkipped(t *testing.T) {
	// row.Caps=true but the first segment's effective bg is empty
	// — this branch is unreachable through the standard public path
	// (renderRowPowerline always passes powerlineActive=true to
	// effectiveSegmentBg, which falls through to defaultPalette).
	// This test exists only to document the guard and ensure no
	// panic when the branch is hit through future internal changes.
	row := Row{
		Caps:     true,
		Segments: []Segment{{Type: "text", Label: "A"}},
	}
	env := renderEnv{colorEnabled: true}
	_ = renderRowPowerline(&payload{}, row, env) // does not panic
}

func TestSegmentAlignRight_NaturalMode_PadsBetweenLeftAndRight(t *testing.T) {
	// Natural mode, Width=20, Margin=0, default separator " | ".
	// Left "L" (1 col), right "R" (1 col) → padding = 20 - 1 - 1 = 18 spaces.
	cfg := Config{
		Width:  20,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "L"},
			{Type: "text", Label: "R", Align: "right"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "L" + strings.Repeat(" ", 18) + "R"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSegmentAlignRight_NaturalMode_UnknownWidthFallsBackInline(t *testing.T) {
	// Width unset and TTY detection fails → ttyCols=0 → padding cannot
	// be computed, so the row degrades to the standard left-joined form.
	prevDev := devTTYWinsizeReader
	prevStat := procStatReader
	defer func() { devTTYWinsizeReader = prevDev; procStatReader = prevStat }()
	devTTYWinsizeReader = func() (int, int, bool) { return 0, 0, false }
	procStatReader = func(int) ([]byte, error) { return nil, errors.New("stub") }

	cfg := Config{
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "L"},
			{Type: "text", Label: "R", Align: "right"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "L" + defaultSeparator + "R"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSegmentAlignRight_NaturalMode_OverflowFallsBackInline(t *testing.T) {
	// Width=1 — left "L" + right "R" need at least 2 cols → negative
	// slack → fall back to inline join, do not corrupt the row by
	// inserting negative padding.
	cfg := Config{
		Width:  1,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "L"},
			{Type: "text", Label: "R", Align: "right"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "L" + defaultSeparator + "R"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSegmentAlignRight_NaturalMode_FirstSegmentRightAlignedFillsLeftPadding(t *testing.T) {
	// When the first segment is right-aligned, the entire row sits flush
	// right — equivalent to Row.Align="right" but per-segment.
	cfg := Config{
		Width:  10,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "X", Align: "right"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := strings.Repeat(" ", 9) + "X"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSegmentAlignRight_NaturalMode_MultipleRightAlignedStayGrouped(t *testing.T) {
	// Once a segment carries Align="right", every subsequent segment in
	// the same row joins the right group regardless of its own Align.
	cfg := Config{
		Width:  20,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "L"},
			{Type: "text", Label: "R1", Align: "right"},
			{Type: "text", Label: "R2"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// left = "L" (1), right = "R1" + sep + "R2" = "R1 | R2" (7).
	// pad = 20 - 1 - 7 = 12.
	want := "L" + strings.Repeat(" ", 12) + "R1" + defaultSeparator + "R2"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSegmentAlignRight_Powerline_PaddingInheritsPrevBg(t *testing.T) {
	// Powerline mode: the padding between left-group and right-group is
	// filled with the bg of the last left-aligned segment so it reads as
	// that segment extending visually. The normal chevron transition
	// then carries left.bg → right.bg as usual.
	cfg := Config{
		Powerline: true,
		Margin:    new(0),
		Width:     20,
		Rows: []Row{{
			Palette: []string{"237", "238"},
			Segments: []Segment{
				{Type: "text", Label: "L"},
				{Type: "text", Label: "R", Align: "right"},
			},
		}},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// usable=20, left body "L" (1), right body "R" (1), separator cost
	// (pre-space + chevron + post-space) = 3 → padding = 20 - 1 - 1 - 3 = 15.
	// The padding spaces must be emitted in bg=237 (left segment's bg)
	// — search for "<bg237><15 spaces>" sequence.
	padSeq := "\x1b[48;5;237m" + strings.Repeat(" ", 15)
	if !strings.Contains(got, padSeq) {
		t.Errorf("expected 15 spaces in bg=237 between L and chevron, got %q", got)
	}
	// The right segment's body must appear after the chevron transition.
	if !strings.Contains(got, "R") {
		t.Errorf("right segment body missing, got %q", got)
	}
	// Stripped output should be exactly: "L" + 15 spaces + " <chev> " + "R"
	// + no trailing padding (right segment is flush right).
	stripped := stripANSI(got)
	want := "L" + strings.Repeat(" ", 15) + " " + powerlineThinGlyph + " " + "R"
	if stripped != want {
		t.Errorf("stripped\ngot  %q\nwant %q", stripped, want)
	}
}

func TestSegmentAlignRight_Powerline_NoEndPaddingWhenRightAligned(t *testing.T) {
	// When a right-aligned segment exists, the row's trailing padding is
	// already consumed by the gap between left and right — there must be
	// no additional trailing whitespace after the right segment.
	cfg := Config{
		Powerline: true,
		Margin:    new(0),
		Width:     20,
		Rows: []Row{{
			Palette: []string{"237", "238"},
			Segments: []Segment{
				{Type: "text", Label: "L"},
				{Type: "text", Label: "R", Align: "right"},
			},
		}},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	stripped := stripANSI(got)
	if strings.HasSuffix(stripped, " ") {
		t.Errorf("right-aligned row must not have trailing padding, got %q", stripped)
	}
	if !strings.HasSuffix(stripped, "R") {
		t.Errorf("right-aligned row must end with right segment body, got %q", stripped)
	}
}

func TestSegmentAlignRight_Powerline_OverflowFallsBackInline(t *testing.T) {
	// When content + chevron cost exceeds usable cols, padding would be
	// negative — the row falls back to inline rendering (no padding, no
	// truncation) so the user can still see all segments.
	cfg := Config{
		Powerline: true,
		Margin:    new(0),
		Width:     5,
		Rows: []Row{{
			Palette: []string{"237", "238"},
			Segments: []Segment{
				{Type: "text", Label: "LLL"},
				{Type: "text", Label: "RRR", Align: "right"},
			},
		}},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	stripped := stripANSI(got)
	// No extra padding inserted between the segments — just the
	// standard chevron transition.
	want := "LLL" + " " + powerlineThinGlyph + " " + "RRR"
	if stripped != want {
		t.Errorf("stripped\ngot  %q\nwant %q", stripped, want)
	}
}

// --- schema health detection ------------------------------------------------

func TestDetectSchemaIssue_TrueOnTopLevelError(t *testing.T) {
	errs := parseErrors{topLevel: errors.New("any parse error")}
	if !detectSchemaIssue(&payload{}, errs) {
		t.Error("top-level parse error must always be flagged as a schema issue")
	}
}

func TestDetectSchemaIssue_TrueOnFieldError(t *testing.T) {
	// Critical fields are all set — only a per-field type error
	// should trip the indicator.
	p := payload{
		SessionID: "s",
		Model:     modelF{DisplayName: "Opus"},
		Workspace: workspace{CurrentDir: "/x"},
	}
	errs := parseErrors{fieldErrors: map[string]error{"cost": errors.New("type error")}}
	if !detectSchemaIssue(&p, errs) {
		t.Error("a per-field type error must trip the indicator even when critical fields are filled")
	}
}

func TestDetectSchemaIssue_TrueOnMissingCriticalFields(t *testing.T) {
	cases := []struct {
		name string
		p    payload
	}{
		{"all empty", payload{}},
		{"no session_id", payload{Model: modelF{DisplayName: "Opus"}, Workspace: workspace{CurrentDir: "/x"}}},
		{"no model.display_name", payload{SessionID: "s", Workspace: workspace{CurrentDir: "/x"}}},
		{"no workspace.current_dir", payload{SessionID: "s", Model: modelF{DisplayName: "Opus"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !detectSchemaIssue(&tc.p, parseErrors{}) {
				t.Errorf("expected schema issue for %s", tc.name)
			}
		})
	}
}

func TestDetectSchemaIssue_FalseOnFullPayload(t *testing.T) {
	p := payload{
		SessionID: "s",
		Model:     modelF{DisplayName: "Opus"},
		Workspace: workspace{CurrentDir: "/x"},
	}
	if detectSchemaIssue(&p, parseErrors{}) {
		t.Error("valid payload must not be flagged")
	}
}

func TestRender_SchemaHealthHiddenOnValidPayload(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "☠") {
		t.Errorf("default layout must not show ☠ when the payload is valid:\n%s", got)
	}
}

func TestRender_SchemaHealthVisibleOnMissingSessionID(t *testing.T) {
	// model + workspace present, session_id missing — schema_health
	// must fire.
	raw := []byte(`{"model":{"display_name":"Opus 4.7"},"workspace":{"current_dir":"/tmp"}}`)
	got, err := Render(Options{NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "☠") {
		t.Errorf("default layout must show ☠ when session_id is missing:\n%s", got)
	}
}

func TestRender_SchemaHealthVisibleOnParseFailure(t *testing.T) {
	got, err := Render(Options{NoColor: true}, []byte("not json"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "☠") {
		t.Errorf("default layout must show ☠ when the JSON parse fails:\n%s", got)
	}
}

func TestRender_SchemaHealthOnlyInDefaultLayoutByDefault(t *testing.T) {
	// A user config without a schema_health segment must NOT show the
	// indicator even when the payload is broken — the segment is
	// opt-in.
	cfg := Config{
		Margin: new(0),
		Rows: []Row{
			{Segments: []Segment{{Type: "text", Label: "static"}}},
		},
	}
	raw := []byte(`{"model":{"display_name":"X"}}`) // missing session_id + cwd
	got, err := Render(Options{NoColor: true, Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "☠") {
		t.Errorf("user config without schema_health segment must not show ☠:\n%s", got)
	}
}

// --- parsePayload (per-segment isolation) -----------------------------------

func TestParsePayload_TopLevelErrorOnNonObject(t *testing.T) {
	p, errs, _ := parsePayload([]byte("not json"))
	if errs.topLevel == nil {
		t.Error("expected topLevel error on garbage input")
	}
	if len(errs.fieldErrors) != 0 {
		t.Errorf("fieldErrors must be empty when topLevel fails, got %v", errs.fieldErrors)
	}
	if p.SessionID != "" || p.Model.DisplayName != "" {
		t.Error("payload should be zero-valued on top-level failure")
	}
}

func TestParsePayload_TopLevelErrorOnArray(t *testing.T) {
	_, errs, _ := parsePayload([]byte(`[1,2,3]`))
	if errs.topLevel == nil {
		t.Error("expected topLevel error on JSON array (we want a top-level object)")
	}
}

func TestParsePayload_AllValidNoErrors(t *testing.T) {
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"cost": {"total_cost_usd": 1.23},
		"context_window": {"used_percentage": 50}
	}`)
	p, errs, _ := parsePayload(raw)
	if errs.hasIssue() {
		t.Errorf("expected no issues, got %+v", errs)
	}
	if p.SessionID != "s" || p.Model.DisplayName != "Opus" || p.Workspace.CurrentDir != "/x" {
		t.Errorf("critical fields not populated: %+v", p)
	}
	if p.Cost.TotalCostUSD != 1.23 {
		t.Errorf("cost not populated: %+v", p.Cost)
	}
	if p.Context.UsedPercentage != 50 {
		t.Errorf("context_window not populated: %+v", p.Context)
	}
}

func TestParsePayload_FieldErrorIsolatedFromOtherFields(t *testing.T) {
	// cost.total_cost_usd is a string but the rest is fine. The
	// per-segment unmarshal must capture the cost error and leave all
	// other fields populated normally.
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"cost": {"total_cost_usd": "not a number"},
		"context_window": {"used_percentage": 50, "context_window_size": 1000}
	}`)
	p, errs, _ := parsePayload(raw)
	if errs.topLevel != nil {
		t.Errorf("topLevel should be nil — only the cost field is broken: %v", errs.topLevel)
	}
	if _, ok := errs.fieldErrors["cost"]; !ok {
		t.Errorf("expected fieldErrors[cost]; got %v", errs.fieldErrors)
	}
	if p.Model.DisplayName != "Opus" {
		t.Errorf("model must survive a cost error: %+v", p.Model)
	}
	if p.Context.UsedPercentage != 50 {
		t.Errorf("context_window must survive a cost error: %+v", p.Context)
	}
	// Cost itself is zero-valued because its unmarshal failed.
	if p.Cost.TotalCostUSD != 0 {
		t.Errorf("cost should be zero after its unmarshal failed: %+v", p.Cost)
	}
}

func TestParsePayload_MissingFieldIsNotAnError(t *testing.T) {
	// A payload that omits "cost" entirely. The field lands at the
	// zero value, but no fieldErrors entry is generated — missing is
	// distinct from broken.
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"}
	}`)
	p, errs, _ := parsePayload(raw)
	if errs.hasIssue() {
		t.Errorf("missing fields must not be reported as issues: %+v", errs)
	}
	if p.Cost.TotalCostUSD != 0 {
		t.Errorf("missing cost field should land at zero: %+v", p.Cost)
	}
}

func TestParsePayload_UnknownKeysAreIgnored(t *testing.T) {
	// Additive schema changes (a new key on Claude Code's side) must
	// not trip the parser — the unknown key is silently dropped.
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"shiny_new_field": {"a": 1, "b": [2, 3]}
	}`)
	_, errs, _ := parsePayload(raw)
	if errs.hasIssue() {
		t.Errorf("unknown keys must be ignored, got %+v", errs)
	}
}

// --- Render() integration: field error contained ---------------------------

func TestRender_FieldErrorIsolationKeepsOtherSegmentsVisible(t *testing.T) {
	// Cost is broken (string instead of number) but everything else
	// is well-formed. Under per-segment isolation:
	//   - cost segment hides (zero data + cost would render "$0.00",
	//     but the broken-cost path leaves it zero so the standard
	//     "$0.00" still appears — that is the documented behaviour,
	//     so we don't assert on cost itself).
	//   - context segment renders with the bar.
	//   - schema_health fires because the per-field error trips the
	//     indicator.
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus 4.7"},
		"workspace": {"current_dir": "/tmp"},
		"cost": {"total_cost_usd": "BROKEN"},
		"context_window": {"used_percentage": 50, "context_window_size": 1000000, "total_input_tokens": 500000}
	}`)
	got, err := Render(Options{NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "☠") {
		t.Errorf("schema_health must fire on a per-field error:\n%s", got)
	}
	// Context segment must still render its bar — proving the field
	// error did not collapse the whole payload.
	if !strings.Contains(got, "500k/1M") {
		t.Errorf("context segment must render even when cost is broken:\n%s", got)
	}
	if !strings.Contains(got, "Opus 4.7") {
		t.Errorf("model must still render:\n%s", got)
	}
}

func TestRender_NoFieldErrorNoIndicator(t *testing.T) {
	// Companion to the above: identical payload but with a valid cost
	// shape. No indicator should appear.
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus 4.7"},
		"workspace": {"current_dir": "/tmp"},
		"cost": {"total_cost_usd": 1.23},
		"context_window": {"used_percentage": 50, "context_window_size": 1000000, "total_input_tokens": 500000}
	}`)
	got, err := Render(Options{NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "☠") {
		t.Errorf("schema_health must stay hidden on a fully valid payload:\n%s", got)
	}
}

func TestExpectedPayloadKeys_ReturnsCopy(t *testing.T) {
	a := ExpectedPayloadKeys()
	b := ExpectedPayloadKeys()
	if &a[0] == &b[0] {
		t.Error("ExpectedPayloadKeys() must return a fresh slice each call")
	}
	a[0] = "mutated"
	c := ExpectedPayloadKeys()
	if c[0] == "mutated" {
		t.Error("mutating the returned slice must not affect the source")
	}
}

func TestExpectedPayloadKeys_MatchesParsePayload(t *testing.T) {
	// Build a payload that includes every expected key set to JSON
	// null. parsePayload should walk each key without producing a
	// field error. If a key in the list is no longer handled by
	// parsePayload (drift), this test would still pass — null unmarshals
	// to zero for any type. The complementary case (parsePayload
	// handling a key that is NOT in the list) is what would silently
	// break ccsb doctor's diff; flag that with a count check below.
	parts := make([]string, 0, len(expectedPayloadKeys))
	for _, k := range expectedPayloadKeys {
		parts = append(parts, fmt.Sprintf("%q: null", k))
	}
	raw := []byte("{" + strings.Join(parts, ",") + "}")
	_, errs, _ := parsePayload(raw)
	if errs.topLevel != nil {
		t.Fatalf("topLevel must be nil for a valid object, got %v", errs.topLevel)
	}
	if len(errs.fieldErrors) != 0 {
		t.Errorf("null payload must not produce field errors; got %v", errs.fieldErrors)
	}
	// Sanity: the list must not be empty (would defeat the purpose).
	if len(expectedPayloadKeys) < 5 {
		t.Errorf("expectedPayloadKeys is suspiciously short (%d), did someone delete entries?", len(expectedPayloadKeys))
	}
}

// --- SchemaVersionOf --------------------------------------------------------

// TestSchemaVersionOf pins the tolerance contract that is the whole
// reason this helper exists next to parsePayload: unlike parsePayload
// it must NOT fail fast on a top-level non-object. Anything that is not
// a JSON object carrying a string schema_version yields "" — never a
// panic, never a stale value — so statusline's version diff simply sees
// "no version" for malformed payloads.
func TestSchemaVersionOf(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"object with version", `{"schema_version":"2.1"}`, "2.1"},
		{"object without version", `{}`, ""},
		{"top-level array", `[1,2,3]`, ""},
		{"not json at all", `not json`, ""},
		{"wrong field type", `{"schema_version":123}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SchemaVersionOf([]byte(c.raw)); got != c.want {
				t.Errorf("SchemaVersionOf(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// --- Diagnose ---------------------------------------------------------------

func TestDiagnose_ValidPayloadNoIssue(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`)
	d := Diagnose(raw)
	if d.Issue() {
		t.Errorf("valid payload must not trip Issue(): %+v", d)
	}
	if d.TopLevelError != nil || len(d.FieldErrors) > 0 || len(d.MissingCritical) > 0 {
		t.Errorf("expected clean diagnostic, got %+v", d)
	}
}

func TestDiagnose_TopLevelError(t *testing.T) {
	d := Diagnose([]byte("not json"))
	if d.TopLevelError == nil {
		t.Error("expected TopLevelError set")
	}
	if !d.Issue() {
		t.Error("top-level error must trip Issue()")
	}
	if len(d.FieldErrors) > 0 || len(d.MissingCritical) > 0 || len(d.AdditiveKeys) > 0 {
		t.Errorf("only TopLevelError should be set, got %+v", d)
	}
}

func TestDiagnose_FieldError(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"cost":{"total_cost_usd":"BROKEN"}}`)
	d := Diagnose(raw)
	if _, ok := d.FieldErrors["cost"]; !ok {
		t.Errorf("expected FieldErrors[cost], got %+v", d.FieldErrors)
	}
	if !d.Issue() {
		t.Error("field error must trip Issue()")
	}
}

func TestDiagnose_MissingCritical(t *testing.T) {
	// Only model is present; session_id and workspace.current_dir missing.
	raw := []byte(`{"model":{"display_name":"Opus"}}`)
	d := Diagnose(raw)
	if !slices.Contains(d.MissingCritical, "session_id") {
		t.Errorf("expected session_id in MissingCritical, got %v", d.MissingCritical)
	}
	if !slices.Contains(d.MissingCritical, "workspace.current_dir") {
		t.Errorf("expected workspace.current_dir in MissingCritical, got %v", d.MissingCritical)
	}
	if slices.Contains(d.MissingCritical, "model.display_name") {
		t.Errorf("model.display_name should NOT be in MissingCritical: %v", d.MissingCritical)
	}
	if !d.Issue() {
		t.Error("missing critical must trip Issue()")
	}
}

func TestDiagnose_AdditiveKeysDoNotTripIssue(t *testing.T) {
	// All critical fields present; one additive key. Issue() must
	// stay false — additive keys are informational.
	raw := []byte(`{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"shiny_new":1}`)
	d := Diagnose(raw)
	if d.Issue() {
		t.Errorf("additive key alone must not trip Issue(): %+v", d)
	}
	if !slices.Contains(d.AdditiveKeys, "shiny_new") {
		t.Errorf("expected shiny_new in AdditiveKeys, got %v", d.AdditiveKeys)
	}
}

func TestDiagnose_AdditiveKeysSortedDeterministically(t *testing.T) {
	raw := []byte(`{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"},"zeta":1,"alpha":2,"middle":3}`)
	d := Diagnose(raw)
	want := []string{"alpha", "middle", "zeta"}
	if !slices.Equal(d.AdditiveKeys, want) {
		t.Errorf("AdditiveKeys not sorted: got %v want %v", d.AdditiveKeys, want)
	}
}

func TestDiagnostic_Format_TopLevelErrorShortCircuits(t *testing.T) {
	d := Diagnose([]byte("not json"))
	out := string(d.Format())
	if !strings.Contains(out, "top-level parse error") {
		t.Errorf("Format must mention top-level parse error: %q", out)
	}
}

func TestDiagnostic_Format_IncludesAllSections(t *testing.T) {
	raw := []byte(`{
		"model": {"display_name": "Opus"},
		"workspace": {"current_dir": "/x"},
		"cost": {"total_cost_usd": "BROKEN"},
		"newly_added": 1
	}`) // session_id missing + cost broken + additive key
	d := Diagnose(raw)
	out := string(d.Format())
	for _, want := range []string{
		"missing critical fields",
		"session_id",
		"per-field parse errors",
		"cost:",
		"additive keys",
		"newly_added",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format output missing %q:\n%s", want, out)
		}
	}
}

func TestDiagnostic_Format_HealthyPayloadStillEmitsHeader(t *testing.T) {
	// Caller usually checks Issue() before Format() — but Format on a
	// healthy diagnostic must not panic and should still produce the
	// header line so the file is identifiable.
	raw := []byte(`{"session_id":"s","model":{"display_name":"O"},"workspace":{"current_dir":"/x"}}`)
	out := string(Diagnose(raw).Format())
	if !strings.HasPrefix(out, "ccsb schema diagnostic") {
		t.Errorf("Format output should start with the header line, got %q", out)
	}
}

// --- 0.2.22 responsive row overflow (reflow) -------------------------------

func TestHasAnyWrap(t *testing.T) {
	if hasAnyWrap(nil) {
		t.Error("nil segments must report false")
	}
	if hasAnyWrap([]Segment{{Type: "text"}}) {
		t.Error("no Wrap should report false")
	}
	if !hasAnyWrap([]Segment{{Type: "a"}, {Type: "b", Wrap: true}}) {
		t.Error("any segment with Wrap=true should report true")
	}
}

func TestRowOverflows_FalseWhenTtyColsUnknown(t *testing.T) {
	row := Row{Segments: []Segment{{Type: "text", Label: "AAAAAAAAAAAAAAAA"}}}
	if rowOverflows(&payload{}, row, renderEnv{ttyCols: 0}, false, " | ") {
		t.Error("with ttyCols=0 the function must report false (cannot measure)")
	}
}

func TestRowOverflows_TrueWhenContentExceedsUsable(t *testing.T) {
	// 3 segments of width 4 + 2 separators of width 3 (" | ") = 18 cols.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAA"},
		{Type: "text", Label: "BBBB"},
		{Type: "text", Label: "CCCC"},
	}}
	env := renderEnv{ttyCols: 10, margin: 0}
	if !rowOverflows(&payload{}, row, env, false, " | ") {
		t.Error("18 cols on a 10-col tty must report overflow")
	}
}

func TestRowOverflows_FalseWhenItFits(t *testing.T) {
	row := Row{Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "text", Label: "B"},
	}}
	env := renderEnv{ttyCols: 80, margin: 0}
	if rowOverflows(&payload{}, row, env, false, " | ") {
		t.Error("two short segments on an 80-col tty must NOT overflow")
	}
}

func TestSplitWrap_PartitionsAndClearsWrapFlag(t *testing.T) {
	row := Row{
		Bg:      "234",
		Palette: []string{"234", "235"},
		Caps:    true,
		Segments: []Segment{
			{Type: "model"},
			{Type: "wrap_a", Wrap: true},
			{Type: "context"},
			{Type: "wrap_b", Wrap: true},
		},
	}
	left, wrapped := splitWrap(row)
	if len(left.Segments) != 2 || left.Segments[0].Type != "model" || left.Segments[1].Type != "context" {
		t.Errorf("left segments wrong: %+v", left.Segments)
	}
	if len(wrapped.Segments) != 2 || wrapped.Segments[0].Type != "wrap_a" || wrapped.Segments[1].Type != "wrap_b" {
		t.Errorf("wrapped segments wrong: %+v", wrapped.Segments)
	}
	for _, s := range wrapped.Segments {
		if s.Wrap {
			t.Errorf("moved segment must have Wrap cleared: %+v", s)
		}
	}
	if wrapped.Bg != "234" || wrapped.Caps != true || len(wrapped.Palette) != 2 {
		t.Errorf("wrapped row must inherit row styling: %+v", wrapped)
	}
}

func TestExpandWrappedRows_NoSplitWhenNoWrapSegments(t *testing.T) {
	rows := []Row{{Segments: []Segment{{Type: "text", Label: "x"}}}}
	out := expandWrappedRows(&payload{}, rows, renderEnv{ttyCols: 10}, false, " | ")
	if len(out) != 1 {
		t.Errorf("rows without Wrap must pass through unchanged: got %d", len(out))
	}
}

func TestExpandWrappedRows_NoSplitWhenItFits(t *testing.T) {
	rows := []Row{{Segments: []Segment{
		{Type: "text", Label: "A"},
		{Type: "text", Label: "B", Wrap: true},
	}}}
	out := expandWrappedRows(&payload{}, rows, renderEnv{ttyCols: 80, margin: 0}, false, " | ")
	if len(out) != 1 {
		t.Errorf("row that fits must not be split even with Wrap: got %d", len(out))
	}
}

func TestExpandWrappedRows_SplitsOnOverflow(t *testing.T) {
	rows := []Row{{
		Palette: []string{"234"},
		Caps:    true,
		Segments: []Segment{
			{Type: "text", Label: "LLLLLLLLLL"},
			{Type: "text", Label: "MMMMMMMMMM"},
			{Type: "text", Label: "RRRRRRRRRR", Wrap: true},
		},
	}}
	env := renderEnv{ttyCols: 20, margin: 0}
	out := expandWrappedRows(&payload{}, rows, env, false, " | ")
	if len(out) != 2 {
		t.Fatalf("expected 2 rows after split, got %d", len(out))
	}
	if len(out[0].Segments) != 2 {
		t.Errorf("left row should keep 2 segments, got %+v", out[0].Segments)
	}
	if len(out[1].Segments) != 1 || out[1].Segments[0].Label != "RRRRRRRRRR" {
		t.Errorf("wrapped row should hold the wrap segment, got %+v", out[1].Segments)
	}
	if out[1].Caps != true || len(out[1].Palette) != 1 {
		t.Errorf("new row must inherit styling: caps=%v palette=%v", out[1].Caps, out[1].Palette)
	}
}

func TestExpandWrappedRows_RightAlignedRowsDoNotWrap(t *testing.T) {
	rows := []Row{{
		Align: "right",
		Segments: []Segment{
			{Type: "text", Label: "AAAA"},
			{Type: "text", Label: "BBBB", Wrap: true},
		},
	}}
	env := renderEnv{ttyCols: 4, margin: 0} // way too narrow
	out := expandWrappedRows(&payload{}, rows, env, false, " | ")
	if len(out) != 1 {
		t.Errorf("right-aligned rows must pass through unchanged, got %d", len(out))
	}
}

// --- end-to-end Render() with reflow ---------------------------------------

func TestRender_WrapMovesSegmentsToNewRowOnOverflow(t *testing.T) {
	// Configure a single row with three segments + two wrap-marked
	// extras; force a narrow tty via Config.Width so the row overflows.
	cfg := Config{
		Width:  30,
		Margin: new(0),
		Rows: []Row{{
			Palette: []string{"234"},
			Segments: []Segment{
				{Type: "text", Label: "MODEL"},
				{Type: "text", Label: "CONTEXT-DATA"},
				{Type: "text", Label: "EXTRA-FIVE-H", Wrap: true},
				{Type: "text", Label: "EXTRA-SEVEN-D", Wrap: true},
			},
		}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 rows after wrap, got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "MODEL") || !strings.Contains(lines[0], "CONTEXT-DATA") {
		t.Errorf("row 1 should keep the left segments: %q", lines[0])
	}
	if strings.Contains(lines[0], "EXTRA-FIVE-H") || strings.Contains(lines[0], "EXTRA-SEVEN-D") {
		t.Errorf("row 1 should NOT carry wrap segments: %q", lines[0])
	}
	if !strings.Contains(lines[1], "EXTRA-FIVE-H") || !strings.Contains(lines[1], "EXTRA-SEVEN-D") {
		t.Errorf("row 2 should carry the wrap segments: %q", lines[1])
	}
}

func TestRender_WrapStaysInPlaceWhenRowFits(t *testing.T) {
	cfg := Config{
		Width:  200,
		Margin: new(0),
		Rows: []Row{{
			Segments: []Segment{
				{Type: "text", Label: "A"},
				{Type: "text", Label: "B", Wrap: true},
			},
		}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if strings.Contains(got, "\n") {
		t.Errorf("row that fits must stay one line, got %q", got)
	}
}

// --- 0.2.23 max_width truncation -------------------------------------------

func TestTruncateToWidth_ZeroOrNegativeIsNoOp(t *testing.T) {
	for _, max := range []int{0, -1, -100} {
		got := truncateToWidth("hello world", max)
		if got != "hello world" {
			t.Errorf("max=%d should be no-op, got %q", max, got)
		}
	}
}

func TestTruncateToWidth_ShortStringPassesThrough(t *testing.T) {
	if got := truncateToWidth("short", 100); got != "short" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateToWidth_ExactBoundaryUnchanged(t *testing.T) {
	if got := truncateToWidth("exactlyten", 10); got != "exactlyten" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateToWidth_AppendsEllipsisOnOverflow(t *testing.T) {
	// "claude-cli-status-bar" is 21 cols; truncate to 10 → "claude-cl…"
	got := truncateToWidth("claude-cli-status-bar", 10)
	if got != "claude-cl…" {
		t.Errorf("got %q, want %q", got, "claude-cl…")
	}
	if runewidth.StringWidth(got) != 10 {
		t.Errorf("truncated width = %d, want 10", runewidth.StringWidth(got))
	}
}

func TestTruncateToWidth_CJKHandledCorrectly(t *testing.T) {
	// "你好世界" = 8 cols (4 fullwidth chars). Truncate to 5 should keep
	// 2 chars + ellipsis (which is 1 col), total 5 cols.
	s := "你好世界"
	got := truncateToWidth(s, 5)
	if w := runewidth.StringWidth(got); w > 5 {
		t.Errorf("CJK truncation should not exceed max: %q width %d", got, w)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("CJK truncation should end with ellipsis, got %q", got)
	}
}

func TestRender_MaxWidthTruncatesCwd(t *testing.T) {
	raw := []byte(`{"workspace":{"current_dir":"/home/u/very/long/path/to/project-with-long-name"}}`)
	cfg := Config{
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "cwd", Format: "full", MaxWidth: 12},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if runewidth.StringWidth(got) != 12 {
		t.Errorf("rendered width = %d, want 12; got %q", runewidth.StringWidth(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated cwd should end with ellipsis, got %q", got)
	}
}

func TestRender_MaxWidthIgnoredWhenShortEnough(t *testing.T) {
	raw := []byte(`{"workspace":{"current_dir":"/short"}}`)
	cfg := Config{
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "cwd", Format: "full", MaxWidth: 50},
		}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, raw)
	if got != "/short" {
		t.Errorf("short cwd must pass through unchanged, got %q", got)
	}
}

func TestRender_MaxWidthAppliesToGitBranchToo(t *testing.T) {
	// Use a text segment as a stand-in (git_branch needs a real .git dir).
	cfg := Config{
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "feature/very-long-branch-name", MaxWidth: 10},
		}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if got != "feature/v…" {
		t.Errorf("got %q, want %q", got, "feature/v…")
	}
}

// --- 0.2.24 min_cols conditional include -----------------------------------

func TestRender_MinColsHidesSegmentOnNarrowTerminal(t *testing.T) {
	cfg := Config{
		Width:  60, // tty narrower than the segment's threshold
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "B", MinCols: 80},
			{Type: "text", Label: "C"},
		}}},
	}
	got, err := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "B") {
		t.Errorf("MinCols=80 on a 60-col tty must hide the segment, got %q", got)
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "C") {
		t.Errorf("only the gated segment should disappear, got %q", got)
	}
	// And no double separator either — the hidden segment must also
	// drop its surrounding chevron / separator, just like an empty
	// segment does.
	if strings.Contains(got, "|  |") {
		t.Errorf("hidden segment must not leave a dangling separator: %q", got)
	}
}

func TestRender_MinColsKeepsSegmentOnWideTerminal(t *testing.T) {
	cfg := Config{
		Width:  200,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "A"},
			{Type: "text", Label: "TOKENS", MinCols: 80},
		}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if !strings.Contains(got, "TOKENS") {
		t.Errorf("MinCols=80 on a 200-col tty must keep the segment, got %q", got)
	}
}

func TestRender_MinColsBoundaryIsInclusive(t *testing.T) {
	// MinCols=80, ttyCols=80 → segment stays (gate fires only when
	// ttyCols is strictly less than MinCols).
	cfg := Config{
		Width:  80,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "X", MinCols: 80},
		}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if !strings.Contains(got, "X") {
		t.Errorf("ttyCols == MinCols must keep the segment, got %q", got)
	}
}

func TestRender_MinColsIgnoredWhenTtyColsUnknown(t *testing.T) {
	// Direct renderSegment unit test with env.ttyCols=0 explicitly:
	// Render() goes through discoverTermSize which may pick up a
	// real /dev/tty width in the test environment, so we exercise
	// the gate at the level it actually fires.
	s := Segment{Type: "text", Label: "Y", MinCols: 10000}
	got := renderSegment(&payload{}, s, renderEnv{ttyCols: 0, colorEnabled: false})
	if got != "Y" {
		t.Errorf("MinCols on ttyCols=0 must be a no-op, got %q", got)
	}
}

func TestRender_MinColsZeroIsDisabled(t *testing.T) {
	cfg := Config{
		Width:  10,
		Margin: new(0),
		Rows: []Row{{Segments: []Segment{
			{Type: "text", Label: "Z", MinCols: 0},
		}}},
	}
	got, _ := Render(Options{Config: cfg, NoColor: true}, []byte(`{}`))
	if !strings.Contains(got, "Z") {
		t.Errorf("MinCols=0 must not gate the segment, got %q", got)
	}
}

// --- 0.2.32 shared-palette / per-output-row stride ------------------------

// TestRender_GlobalPaletteDefaultIsNineGreys pins the default global
// palette so a future tweak to the visual identity is a conscious
// breaking change.
func TestRender_GlobalPaletteDefaultIsNineGreys(t *testing.T) {
	want := []string{"232", "233", "234", "235", "236", "237", "238", "239", "240"}
	if len(defaultGlobalPalette) != len(want) {
		t.Fatalf("defaultGlobalPalette length: got %d, want %d", len(defaultGlobalPalette), len(want))
	}
	for i, w := range want {
		if defaultGlobalPalette[i] != w {
			t.Errorf("defaultGlobalPalette[%d]: got %q, want %q", i, defaultGlobalPalette[i], w)
		}
	}
	if defaultPaletteStride != 2 {
		t.Errorf("defaultPaletteStride: got %d, want 2", defaultPaletteStride)
	}
}

// TestRender_DefaultConfigUsesSharedPalette verifies that the default
// config no longer carries per-row Palette entries — everything goes
// through Config.Palette + paletteStart now.
func TestRender_DefaultConfigUsesSharedPalette(t *testing.T) {
	if len(defaultConfig.Palette) == 0 {
		t.Fatalf("defaultConfig.Palette must be set")
	}
	if defaultConfig.PaletteStride != defaultPaletteStride {
		t.Errorf("defaultConfig.PaletteStride: got %d, want %d", defaultConfig.PaletteStride, defaultPaletteStride)
	}
	for i, row := range defaultConfig.Rows {
		if len(row.Palette) != 0 {
			t.Errorf("defaultConfig.Rows[%d].Palette must be empty (uses shared global palette), got %v", i, row.Palette)
		}
	}
}

// TestRender_TwoRowsBgStartsTwoShadesBrighter exercises the wide-tty
// path (no wrap): row 0 starts at globalPalette[0] = 232, row 1 at
// globalPalette[2] = 234 (stride 2).
func TestRender_TwoRowsBgStartsTwoShadesBrighter(t *testing.T) {
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus 4.7"},
		"workspace": {"current_dir": "/tmp/proj"},
		"rate_limits": {
			"five_hour": {"used_percentage": 10, "resets_at": 9999999999},
			"seven_day": {"used_percentage": 20, "resets_at": 9999999999}
		}
	}`)
	// Width 200 keeps row 0 from wrapping.
	cfg := Config{Width: 200}
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d:\n%s", len(rows), got)
	}
	if !strings.Contains(rows[0], "\x1b[48;5;232m") {
		t.Errorf("row 0 first cell must be bg=232 (globalPalette[0]):\n%s", rows[0])
	}
	if !strings.Contains(rows[1], "\x1b[48;5;234m") {
		t.Errorf("row 1 first cell must be bg=234 (globalPalette[2], stride 2):\n%s", rows[1])
	}
}

// TestRender_ThreeRowsAfterWrapStartsMonotonic is the regression test for
// the original bug: when row 0 reflows into two rows because of width,
// the wrap row must NOT restart at the parent's palette[0]. With stride
// 2 and 9 greys, the output rows must start at 232, 234, 236.
func TestRender_ThreeRowsAfterWrapStartsMonotonic(t *testing.T) {
	raw := []byte(`{
		"session_id": "s",
		"model": {"display_name": "Opus 4.7"},
		"workspace": {"current_dir": "/tmp/proj"},
		"rate_limits": {
			"five_hour": {"used_percentage": 42, "resets_at": 9999999999},
			"seven_day": {"used_percentage": 17, "resets_at": 9999999999}
		}
	}`)
	// Width 50 forces the limit_5h / limit_7d wrap-marked segments into
	// their own reflowed row.
	cfg := Config{Width: 50}
	got, err := Render(Options{Config: cfg}, raw)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 3 {
		t.Fatalf("want 3 rows after reflow, got %d:\n%s", len(rows), got)
	}
	// outputIdx * stride: 0, 2, 4 → globalPalette[0,2,4] = 232, 234, 236.
	wants := []string{"\x1b[48;5;232m", "\x1b[48;5;234m", "\x1b[48;5;236m"}
	for i, want := range wants {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d must start with bg %q after reflow:\n%s", i, want, rows[i])
		}
		// No two adjacent output rows may start with the same bg — that
		// was the visible symptom of the bug.
		if i > 0 {
			prev := wants[i-1]
			if strings.HasPrefix(stripANSI(rows[i]), stripANSI(rows[i-1])) {
				t.Errorf("row %d must start brighter than row %d (prev bg %q)", i, i-1, prev)
			}
		}
	}
}

// TestRender_RowPaletteOverridesGlobal verifies that a per-row Palette
// still wins over Config.Palette — the documented escape hatch for
// power users.
func TestRender_RowPaletteOverridesGlobal(t *testing.T) {
	cfg := Config{
		Width:     200,
		Margin:    new(0),
		Powerline: true,
		Palette:   []string{"232", "233", "234"},
		Rows: []Row{
			{
				Palette: []string{"100", "101"}, // overrides global
				Segments: []Segment{
					{Type: "text", Label: "A"},
					{Type: "text", Label: "B"},
				},
			},
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "\x1b[48;5;100m") {
		t.Errorf("Row.Palette[0]=100 must win over Config.Palette:\n%s", got)
	}
	if !strings.Contains(got, "\x1b[48;5;101m") {
		t.Errorf("Row.Palette[1]=101 must win over Config.Palette:\n%s", got)
	}
	if strings.Contains(got, "\x1b[48;5;232m") {
		t.Errorf("Config.Palette must NOT leak through Row.Palette override:\n%s", got)
	}
}

// TestRender_CustomPaletteStride verifies that a non-default stride is
// honoured. Stride 1 → row N starts one shade brighter than row N-1.
func TestRender_CustomPaletteStride(t *testing.T) {
	cfg := Config{
		Width:         200,
		Margin:        new(0),
		Powerline:     true,
		Palette:       []string{"232", "233", "234", "235", "236"},
		PaletteStride: 1,
		Rows: []Row{
			{Segments: []Segment{{Type: "text", Label: "A"}}},
			{Segments: []Segment{{Type: "text", Label: "B"}}},
			{Segments: []Segment{{Type: "text", Label: "C"}}},
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	wants := []string{"232", "233", "234"}
	for i, w := range wants {
		seq := "\x1b[48;5;" + w + "m"
		if !strings.Contains(rows[i], seq) {
			t.Errorf("row %d: want bg %s, got %q", i, w, rows[i])
		}
	}
}

// TestRender_HiddenRowDoesNotSkipPaletteShades verifies that a row
// whose segments all render empty is dropped without consuming a
// palette offset: the first VISIBLE row must still start at palette
// index 0 and the following one exactly one stride later. Keying the
// offset to the configured row index instead would leave a
// stride-sized jump in the gradient.
func TestRender_HiddenRowDoesNotSkipPaletteShades(t *testing.T) {
	cfg := Config{
		Width:         200,
		Margin:        new(0),
		Powerline:     true,
		Palette:       []string{"232", "233", "234", "235", "236"},
		PaletteStride: 1,
		Rows: []Row{
			// output_style hides itself on a payload without an
			// output_style key, so this row emits nothing at all.
			{Segments: []Segment{{Type: "output_style"}}},
			{Segments: []Segment{{Type: "text", Label: "A"}}},
			{Segments: []Segment{{Type: "text", Label: "B"}}},
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 visible rows, got %d (%q)", len(rows), got)
	}
	for i, w := range []string{"232", "233"} {
		if !strings.Contains(rows[i], "\x1b[48;5;"+w+"m") {
			t.Errorf("visible row %d: want bg %s, got %q", i, w, rows[i])
		}
	}
}

// TestRender_PaletteStrideWrapsAroundCleanly verifies the modulo
// behaviour when paletteStart + visibleIndex exceeds the palette
// length: we must rotate, not crash.
func TestRender_PaletteStrideWrapsAroundCleanly(t *testing.T) {
	cfg := Config{
		Width:         200,
		Margin:        new(0),
		Powerline:     true,
		Palette:       []string{"232", "233", "234"},
		PaletteStride: 2,
		Rows: []Row{
			{Segments: []Segment{{Type: "text", Label: "A"}}}, // start 0 → 232
			{Segments: []Segment{{Type: "text", Label: "B"}}}, // start 2 → 234
			{Segments: []Segment{{Type: "text", Label: "C"}}}, // start 4 % 3 = 1 → 233
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	for i, w := range []string{"232", "234", "233"} {
		if !strings.Contains(rows[i], "\x1b[48;5;"+w+"m") {
			t.Errorf("row %d: want bg %s, got %q", i, w, rows[i])
		}
	}
}

// TestRender_PaletteStrideZeroUsesDefault verifies that Config.PaletteStride=0
// falls back to defaultPaletteStride (=2) so old configs that never set the
// field keep getting the documented per-row brighten step.
func TestRender_PaletteStrideZeroUsesDefault(t *testing.T) {
	cfg := Config{
		Width:     200,
		Margin:    new(0),
		Powerline: true,
		Palette:   defaultGlobalPalette,
		// PaletteStride intentionally zero
		Rows: []Row{
			{Segments: []Segment{{Type: "text", Label: "A"}}},
			{Segments: []Segment{{Type: "text", Label: "B"}}},
		},
	}
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "\x1b[48;5;232m") {
		t.Errorf("row 0: want bg 232, got %q", rows[0])
	}
	if !strings.Contains(rows[1], "\x1b[48;5;234m") {
		t.Errorf("row 1 with stride=0 must default to stride=2 → 234, got %q", rows[1])
	}
}

// TestConfig_EffectivePaletteStride documents the stride-resolution
// rules at the field level.
func TestConfig_EffectivePaletteStride(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults", 0, defaultPaletteStride},
		{"positive used", 4, 4},
		{"negative clamps to zero", -3, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Config{PaletteStride: c.in}
			if got := cfg.effectivePaletteStride(); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestConfig_PaletteJSONRoundtrip verifies the new field marshals
// and unmarshals cleanly so users can pin a custom palette in their
// config file.
func TestConfig_PaletteJSONRoundtrip(t *testing.T) {
	in := Config{
		Palette:       []string{"232", "234", "236"},
		PaletteStride: 3,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"palette":["232","234","236"]`) {
		t.Errorf("palette missing from JSON: %s", data)
	}
	if !strings.Contains(string(data), `"palette_stride":3`) {
		t.Errorf("palette_stride missing from JSON: %s", data)
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out.Palette) != 3 || out.Palette[0] != "232" {
		t.Errorf("palette round-trip: got %v", out.Palette)
	}
	if out.PaletteStride != 3 {
		t.Errorf("palette_stride round-trip: got %d", out.PaletteStride)
	}
}

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

func TestApplyShrink_LowersPreExistingMaxWidthOnOverflow(t *testing.T) {
	// Shrink segment already capped at MaxWidth:4 (renderSegment truncates
	// it to 4 cols, so its measured width is 4). Fixed neighbour is 10 cols.
	// Sep " | " = 3. used = 4 + 10 + 3 = 17, usable = 9, deficit = 8.
	// canGive = 4 - 1 = 3, take = min(3, 8) = 3 → MaxWidth lowered to 1.
	row := Row{Segments: []Segment{
		{Type: "text", Label: "AAAAAAAAAA", Shrink: true, MaxWidth: 4},
		{Type: "text", Label: "BBBBBBBBBB"},
	}}
	env := renderEnv{ttyCols: 9, margin: 0}
	out := applyShrink(&payload{}, row, env, false, " | ")
	if out[0].MaxWidth != 1 {
		t.Errorf("pre-existing MaxWidth must be further lowered toward the floor, got %d", out[0].MaxWidth)
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

func TestApplyShrink_PowerlineWithCapsAccounting(t *testing.T) {
	// Powerline mode with Caps: used = shrink(10) + fixed(6) +
	// 1 powerlineSeparatorWidth(3) = 19. usable = ttyCols(14) - 2*margin(0)
	// - 2 (caps) = 12. deficit = 7. canGive = 10 - 1 = 7 → MaxWidth = 3.
	row := Row{
		Caps: true,
		Segments: []Segment{
			{Type: "text", Label: "AAAAAAAAAA", Shrink: true}, // 10
			{Type: "text", Label: "BBBBBB"},                   // 6
		},
	}
	env := renderEnv{ttyCols: 14, margin: 0}
	out := applyShrink(&payload{}, row, env, true /* powerlineActive */, " | ")
	if out[0].MaxWidth != 3 {
		t.Errorf("powerline+caps shrink MaxWidth = %d, want 3", out[0].MaxWidth)
	}
}

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
	line, _, _ := strings.Cut(got, "\n")
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

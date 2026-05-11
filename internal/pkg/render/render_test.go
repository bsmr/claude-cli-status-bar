package render

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRe.ReplaceAllString(s, "")
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
	cfg := Config{Rows: [][]Segment{{{Type: "frobnicate"}}}}
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
		Rows: [][]Segment{
			{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}},
			{{Type: "text", Label: "C"}},
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
	cfg := Config{Rows: [][]Segment{{{Type: "text", Label: "A"}, {Type: "text", Label: "B"}}}}
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
	if !strings.Contains(cleanGot, "$17.70") || !strings.Contains(cleanGot, "5h:7%") {
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
	cfg := Config{Rows: [][]Segment{{
		{Type: "context", Style: "pct", FG: "245",
			Thresholds: []Threshold{
				{Min: 70, FG: "136"},
				{Min: 90, FG: "160"},
			}},
	}}}
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

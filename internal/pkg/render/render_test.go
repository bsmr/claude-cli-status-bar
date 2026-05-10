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

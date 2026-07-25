package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderModel_DefaultDisplayName(t *testing.T) {
	p := &payload{Model: modelF{DisplayName: "Opus 4.7"}}
	if got := renderModel(p, Segment{}, renderEnv{}); got != "Opus 4.7" {
		t.Errorf("got %q, want Opus 4.7", got)
	}
}

func TestRenderModel_With1MFlagAppendsWhenExceeds(t *testing.T) {
	p := &payload{Model: modelF{DisplayName: "Opus 4.7"}, Exceeds200kT: true}
	if got := renderModel(p, Segment{Show1MFlag: true}, renderEnv{}); got != "Opus 4.7 1M" {
		t.Errorf("got %q, want Opus 4.7 1M", got)
	}
}

func TestRenderModel_With1MFlagButNotExceedingDoesNotAppend(t *testing.T) {
	p := &payload{Model: modelF{DisplayName: "Sonnet"}, Exceeds200kT: false}
	if got := renderModel(p, Segment{Show1MFlag: true}, renderEnv{}); got != "Sonnet" {
		t.Errorf("got %q, want Sonnet", got)
	}
}

func TestRenderModel_StripsParentheticalSuffix(t *testing.T) {
	// e.g. "Opus 4.7 (1M context)" -> "Opus 4.7"; the (1M) is added back
	// only when Show1MFlag and Exceeds200kT both true.
	p := &payload{Model: modelF{DisplayName: "Opus 4.7 (1M context)"}, Exceeds200kT: true}
	if got := renderModel(p, Segment{Show1MFlag: true}, renderEnv{}); got != "Opus 4.7 1M" {
		t.Errorf("got %q, want Opus 4.7 1M", got)
	}
}

func TestRenderEffort_PrefixesWithLabel(t *testing.T) {
	p := &payload{Effort: effortF{Level: "xhigh"}}
	if got := renderEffort(p, Segment{}, renderEnv{}); got != "effort: xhigh" {
		t.Errorf("got %q", got)
	}
}

func TestRenderEffort_EmptyLevelHidesSegment(t *testing.T) {
	if got := renderEffort(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("empty level should hide, got %q", got)
	}
}

func TestRenderEffort_LabelOverride(t *testing.T) {
	p := &payload{Effort: effortF{Level: "xhigh"}}
	if got := renderEffort(p, Segment{Label: "E"}, renderEnv{}); got != "E: xhigh" {
		t.Errorf("got %q", got)
	}
}

func TestRenderSessionName_Plain(t *testing.T) {
	p := &payload{SessionName: "Skeleton implementieren"}
	if got := renderSessionName(p, Segment{}, renderEnv{}); got != "Skeleton implementieren" {
		t.Errorf("got %q", got)
	}
}

func TestRenderOutputStyle_PrefixesWithLabel(t *testing.T) {
	p := &payload{OutputStyle: outputF{Name: "default"}}
	if got := renderOutputStyle(p, Segment{}, renderEnv{}); got != "style: default" {
		t.Errorf("got %q", got)
	}
}

// TestRenderOutputStyle_EmptyNameIsHidden pins the hide-when-no-data guard:
// a payload without output_style must drop the segment entirely rather than
// emit a bare "style: " label.
func TestRenderOutputStyle_EmptyNameIsHidden(t *testing.T) {
	if got := renderOutputStyle(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	// A custom label must not resurrect the segment either.
	if got := renderOutputStyle(&payload{}, Segment{Label: "os"}, renderEnv{}); got != "" {
		t.Errorf("labelled: got %q, want empty", got)
	}
}

func TestRenderOutputStyle_CustomLabel(t *testing.T) {
	p := &payload{OutputStyle: outputF{Name: "default"}}
	if got := renderOutputStyle(p, Segment{Label: "os"}, renderEnv{}); got != "os: default" {
		t.Errorf("got %q, want \"os: default\"", got)
	}
}

func TestRenderCwd_BasenameOnly(t *testing.T) {
	p := &payload{Workspace: workspace{CurrentDir: "/home/u/projects/foo"}}
	if got := renderCwd(p, Segment{}, renderEnv{}); got != "foo" {
		t.Errorf("got %q, want foo", got)
	}
}

func TestRenderCwd_FormatFullShowsAbsolutePath(t *testing.T) {
	p := &payload{Workspace: workspace{CurrentDir: "/home/u/projects/foo"}}
	if got := renderCwd(p, Segment{Format: "full"}, renderEnv{}); got != "/home/u/projects/foo" {
		t.Errorf("got %q", got)
	}
}

func TestRenderCost_Default(t *testing.T) {
	p := &payload{Cost: costF{TotalCostUSD: 17.7024}}
	if got := renderCost(p, Segment{}, renderEnv{}); got != "$17.70" {
		t.Errorf("got %q, want $17.70", got)
	}
}

func TestRenderCost_FormatOverride(t *testing.T) {
	p := &payload{Cost: costF{TotalCostUSD: 17.7024}}
	if got := renderCost(p, Segment{Format: "$%.4f"}, renderEnv{}); got != "$17.7024" {
		t.Errorf("got %q", got)
	}
}

func TestRenderCost_ZeroIsRendered(t *testing.T) {
	if got := renderCost(&payload{}, Segment{}, renderEnv{}); got != "$0.00" {
		t.Errorf("got %q", got)
	}
}

func TestRenderDuration_HoursAndMinutes(t *testing.T) {
	p := &payload{Cost: costF{TotalDurationMS: 7_800_232}} // ~2h10m
	if got := renderDuration(p, Segment{}, renderEnv{}); got != "2h10m" {
		t.Errorf("got %q, want 2h10m", got)
	}
}

func TestRenderDuration_MinutesOnly(t *testing.T) {
	p := &payload{Cost: costF{TotalDurationMS: 300_000}} // 5m
	if got := renderDuration(p, Segment{}, renderEnv{}); got != "5m" {
		t.Errorf("got %q", got)
	}
}

func TestRenderDuration_SecondsOnly(t *testing.T) {
	p := &payload{Cost: costF{TotalDurationMS: 12_345}} // 12s
	if got := renderDuration(p, Segment{}, renderEnv{}); got != "12s" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLines_BothNonZero(t *testing.T) {
	p := &payload{Cost: costF{TotalLinesAdded: 2813, TotalLinesRemoved: 136}}
	if got := renderLines(p, Segment{}, renderEnv{}); got != "+2813 −136" {
		t.Errorf("got %q (note unicode minus)", got)
	}
}

func TestRenderLines_ZeroBoth_Hidden(t *testing.T) {
	if got := renderLines(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("zero/zero should hide, got %q", got)
	}
}

func TestRenderContext_BarPlusPctDefault(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	p.Context.ContextWindowSize = 1_000_000
	p.Context.TotalInputTokens = 273_000
	got := renderContext(p, Segment{Style: "bar+pct"}, renderEnv{})
	// 27% of 16-cell bar: 27×64/100=17.28 → 17 quarters → 4 full + remainder 1 (◔)
	if got != "●●●●◔○○○○○○○○○○○ 27% 273k/1M" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_PctOnlyStyle(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	if got := renderContext(p, Segment{Style: "pct"}, renderEnv{}); got != "27%" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_BarOnlyStyle(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	if got := renderContext(p, Segment{Style: "bar"}, renderEnv{}); got != "●●●●◔○○○○○○○○○○○" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_EmptyStyleDefaultsToBarPct(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 50
	p.Context.ContextWindowSize = 200_000
	p.Context.TotalInputTokens = 100_000
	if got := renderContext(p, Segment{}, renderEnv{}); got != "●●●●●●●●○○○○○○○○ 50% 100k/200k" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_HiddenWhenNoData(t *testing.T) {
	if got := renderContext(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("expected empty when no data, got %q", got)
	}
}

func contextPayload50() *payload {
	p := &payload{}
	p.Context.UsedPercentage = 50
	p.Context.ContextWindowSize = 200_000
	p.Context.TotalInputTokens = 100_000
	return p
}

func TestRenderContext_TokenPositionBefore(t *testing.T) {
	got := renderContext(contextPayload50(), Segment{Style: "bar+pct", TokenPosition: "before"}, renderEnv{})
	if got != "100k/200k ●●●●●●●●○○○○○○○○ 50%" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_TokenPositionHidden(t *testing.T) {
	got := renderContext(contextPayload50(), Segment{Style: "bar+pct", TokenPosition: "hidden"}, renderEnv{})
	if got != "●●●●●●●●○○○○○○○○ 50%" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_TokenPositionAfterEqualsDefault(t *testing.T) {
	p := contextPayload50()
	after := renderContext(p, Segment{Style: "bar+pct", TokenPosition: "after"}, renderEnv{})
	def := renderContext(p, Segment{Style: "bar+pct"}, renderEnv{})
	if after != def || def != "●●●●●●●●○○○○○○○○ 50% 100k/200k" {
		t.Errorf("after=%q def=%q", after, def)
	}
}

func TestRenderContext_TokenPositionUnknownFallsBackToAfter(t *testing.T) {
	got := renderContext(contextPayload50(), Segment{Style: "bar+pct", TokenPosition: "sideways"}, renderEnv{})
	if got != "●●●●●●●●○○○○○○○○ 50% 100k/200k" {
		t.Errorf("got %q", got)
	}
}

func TestWrapPart(t *testing.T) {
	if got := wrapPart("x", "1", "2", false); got != "x" {
		t.Errorf("colour off: %q", got)
	}
	if got := wrapPart("x", "", "2", true); got != "x" {
		t.Errorf("empty innerFG: %q", got)
	}
	if got := wrapPart("x", "5", "5", true); got != "x" {
		t.Errorf("innerFG==ambient: %q", got)
	}
	if got := wrapPart("x", "999", "2", true); got != "x" {
		t.Errorf("invalid innerFG: %q", got)
	}
	if got := wrapPart("x", "1", "2", true); got != fg256("1")+"x"+fg256("2") {
		t.Errorf("wrap+restore: %q", got)
	}
	if got := wrapPart("x", "1", "", true); got != fg256("1")+"x"+"\x1b[39m" {
		t.Errorf("default close: %q", got)
	}
}

func TestRenderContext_NoPartColorsUnchanged(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 50
	p.Context.ContextWindowSize = 200_000
	p.Context.TotalInputTokens = 100_000
	// colour on, but no bar_fg/threshold_target -> plain body, no escapes.
	got := renderContext(p, Segment{Style: "bar+pct"}, renderEnv{colorEnabled: true})
	if got != "●●●●●●●●○○○○○○○○ 50% 100k/200k" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_BarFGColorsBarOnly(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 50
	got := renderContext(p, Segment{Style: "bar", BarFG: "245"}, renderEnv{colorEnabled: true})
	want := fg256("245") + "●●●●●●●●○○○○○○○○" + "\x1b[39m"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRenderContext_BarThresholdsReactive(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 95
	s := Segment{Type: "context", Style: "bar", BarWidth: 4, BarThresholds: []Threshold{{Min: 0, FG: "2"}, {Min: 90, FG: "1"}}}
	got := renderContext(p, s, renderEnv{colorEnabled: true})
	want := fg256("1") + "●●●◕" + "\x1b[39m" // 95% -> highest min 90 -> "1"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRenderContext_DimBarBrightNumberCompose(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 95
	p.Context.ContextWindowSize = 200_000
	p.Context.TotalInputTokens = 100_000
	s := Segment{
		Type: "context", Style: "bar+pct", BarWidth: 4, FG: "245", ThresholdTarget: "pct",
		Thresholds: []Threshold{{Min: 90, FG: "1"}}, BarFG: "240",
	}
	got := renderContext(p, s, renderEnv{colorEnabled: true})
	// bar dim 240 -> back to ambient 245; number 95% bright 1 -> back to 245.
	want := fg256("240") + "●●●◕" + fg256("245") + " " +
		fg256("1") + "95%" + fg256("245") + " 100k/200k"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderLimit_LabelFGColorsLabelOnly(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 40, colorEnabled: true} // 60s -> 1m
	got := renderLimit5h(p, Segment{LabelFG: "245"}, env)
	want := fg256("245") + "5h:" + "\x1b[39m" + " 50% · 1m"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRenderLimit_BarFGColorsBarOnly(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	got := renderLimit5h(p, Segment{Style: "bar+pct", BarWidth: 4, BarFG: "245"}, renderEnv{colorEnabled: true})
	want := "5h: " + fg256("245") + "●●○○" + "\x1b[39m" + " 50%"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestMakeBarGlyphs_CircleSequence(t *testing.T) {
	// 4-cell bar: 16 quarter-steps total; each quarter-step = 6.25 pp.
	// 1 full cell = 4 quarters = 25 pp.
	for _, tc := range []struct {
		pct  float64
		want string
	}{
		{0, "○○○○"},
		{25, "●○○○"},    // exactly 1 full cell (4 quarters)
		{50, "●●○○"},    // 2 full cells
		{100, "●●●●"},   // all full
		{6.25, "◔○○○"},  // 0 full, remainder 1
		{12.5, "◑○○○"},  // 0 full, remainder 2
		{18.75, "◕○○○"}, // 0 full, remainder 3
		{31.25, "●◔○○"}, // 1 full, remainder 1
	} {
		if got := makeBarGlyphs(tc.pct, 4, circleSteps); got != tc.want {
			t.Errorf("circles %.4f%%: got %q, want %q", tc.pct, got, tc.want)
		}
	}
}

// TestFormatTokens_AllBranches covers every branch of the compaction,
// including the two the segment tests never reach: the plain "%d" case for
// the sub-1000 counts of an early session, and the fractional "%.1fM" the
// docstring promises.
func TestFormatTokens_AllBranches(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{999, "999"},
		{1234, "1k"},
		{273000, "273k"},
		{1000000, "1M"},
		{1500000, "1.5M"},
	} {
		if got := formatTokens(tc.n); got != tc.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// joinRamp compares glyph ramps without pulling in reflect/strings.
func joinRamp(s []string) string {
	out := ""
	for _, x := range s {
		out += x + "|"
	}
	return out
}

func TestMakeBarGlyphs_Blocks(t *testing.T) {
	for _, tc := range []struct {
		pct  float64
		want string
	}{
		{0, "░░░░"},
		{50, "██░░"},
		{100, "████"},
		// 4 cells × 8 sub-steps = 32 levels; 12.5% → 4 → 0 full cells,
		// remainder 4 → blockSteps[4] = "▌" in the first cell.
		{12.5, "▌░░░"},
	} {
		if got := makeBarGlyphs(tc.pct, 4, blockSteps); got != tc.want {
			t.Errorf("blocks %.4f%%: got %q, want %q", tc.pct, got, tc.want)
		}
	}
}

func TestMakeBarGlyphs_CustomTwoStep(t *testing.T) {
	if got := makeBarGlyphs(50, 4, []string{".", "#"}); got != "##.." {
		t.Errorf("got %q", got)
	}
}

func TestMakeBarGlyphs_ShortRampFallsBackToCircles(t *testing.T) {
	if got := makeBarGlyphs(50, 4, []string{"x"}); got != makeBarGlyphs(50, 4, circleSteps) {
		t.Errorf("short ramp should fall back to circles, got %q", got)
	}
}

func TestBarRamp_Resolution(t *testing.T) {
	if joinRamp(barRamp(Segment{})) != joinRamp(circleSteps) {
		t.Error("default should be circles")
	}
	if joinRamp(barRamp(Segment{BarStyle: "blocks"})) != joinRamp(blockSteps) {
		t.Error("blocks preset")
	}
	if joinRamp(barRamp(Segment{BarStyle: "nonsense"})) != joinRamp(circleSteps) {
		t.Error("unknown style falls back to circles")
	}
	custom := []string{".", "#"}
	if joinRamp(barRamp(Segment{BarGlyphs: custom})) != joinRamp(custom) {
		t.Error("bar_glyphs overrides")
	}
	if joinRamp(barRamp(Segment{BarGlyphs: []string{"x"}, BarStyle: "blocks"})) != joinRamp(blockSteps) {
		t.Error("too-short bar_glyphs falls back to bar_style")
	}
}

func TestRenderContext_BarStyleBlocks(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 50
	if got := renderContext(p, Segment{Style: "bar", BarStyle: "blocks"}, renderEnv{}); got != "████████░░░░░░░░" {
		t.Errorf("got %q", got)
	}
}

// TestRenderLimit_BarStyleBlocks mirrors TestRenderContext_BarStyleBlocks
// for the limit path, which also switched to the configurable ramp — a
// regression that dropped the ramp on renderLimit would go uncaught otherwise.
func TestRenderLimit_BarStyleBlocks(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	if got := renderLimit5h(p, Segment{Style: "bar", BarStyle: "blocks"}, renderEnv{}); got != "5h: ████████░░░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLimit5h_PercentAndCountdown(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 7.0
	p.Limits.FiveHour.ResetsAt = 1_778_412_000

	env := renderEnv{nowUnix: 1_778_412_000 - (4*3600 + 23*60)}
	got := renderLimit5h(p, Segment{}, env)
	if got != "5h: 7% · 4h23m" {
		t.Errorf("got %q, want 5h: 7%% · 4h23m", got)
	}
}

func TestRenderLimit5h_LabelOverride(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 100 - 60} // 1m
	if got := renderLimit5h(p, Segment{Label: "WIN"}, env); got != "WIN: 50% · 1m" {
		t.Errorf("got %q, want WIN: 50%% · 1m", got)
	}
}

func TestRenderLimit5h_HiddenWhenZeroAndNoReset(t *testing.T) {
	if got := renderLimit5h(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLimit7d_DaysAndHours(t *testing.T) {
	p := &payload{}
	p.Limits.SevenDay.UsedPercentage = 30
	p.Limits.SevenDay.ResetsAt = 1_000_000
	env := renderEnv{nowUnix: 1_000_000 - (5*86400 + 2*3600)} // 5d2h
	if got := renderLimit7d(p, Segment{}, env); got != "7d: 30% · 5d2h" {
		t.Errorf("got %q, want 7d: 30%% · 5d2h", got)
	}
}

func TestRenderLimit_NegativeRemainingShowsNow(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 10
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 200} // already past
	if got := renderLimit5h(p, Segment{}, env); got != "5h: 10% · now" {
		t.Errorf("got %q, want 5h: 10%% · now", got)
	}
}

func TestRenderLimit5h_BarStyle(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50 // 8/16 cells
	p.Limits.FiveHour.ResetsAt = 1000
	env := renderEnv{nowUnix: 500}
	got := renderLimit5h(p, Segment{Style: "bar"}, env)
	want := "5h: ●●●●●●●●○○○○○○○○"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderLimit5h_BarPctStyleIncludesCountdown(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	p.Limits.FiveHour.ResetsAt = 500 + 60
	env := renderEnv{nowUnix: 500}
	got := renderLimit5h(p, Segment{Style: "bar+pct"}, env)
	want := "5h: ●●●●●●●●○○○○○○○○ 50% · 1m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderLimit5h_BarPctWithoutResetOmitsCountdown(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	// ResetsAt zero
	env := renderEnv{nowUnix: 500}
	got := renderLimit5h(p, Segment{Style: "bar+pct"}, env)
	want := "5h: ●●●●●●●●○○○○○○○○ 50%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEffectiveBarCells_FallbackAndOverride(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  int
	}{
		{0, barCells},  // unset → default
		{-3, barCells}, // negative → default
		{8, 8},         // explicit override
		{1, 1},         // minimum sensible value still honoured
	} {
		if got := effectiveBarCells(Segment{BarWidth: tc.width}); got != tc.want {
			t.Errorf("effectiveBarCells(BarWidth=%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestRenderContext_BarWidthOverride(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	p.Context.ContextWindowSize = 1_000_000
	p.Context.TotalInputTokens = 273_000
	// Same data as TestRenderContext_BarPlusPctDefault but at 8 cells:
	// 27% of 8-cell bar → 27×32/100=8.64 → 9 quarters → 2 full + remainder 1 (◔).
	got := renderContext(p, Segment{Style: "bar+pct", BarWidth: 8}, renderEnv{})
	if got != "●●◔○○○○○ 27% 273k/1M" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_BarWidthZeroUsesDefault(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	withZero := renderContext(p, Segment{Style: "bar", BarWidth: 0}, renderEnv{})
	withDefault := renderContext(p, Segment{Style: "bar"}, renderEnv{})
	if withZero != withDefault {
		t.Errorf("BarWidth=0 should match default: %q vs %q", withZero, withDefault)
	}
}

func TestRenderLimit5h_BarWidthOverride(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50 // 4/8 cells
	p.Limits.FiveHour.ResetsAt = 1000
	env := renderEnv{nowUnix: 500}
	got := renderLimit5h(p, Segment{Style: "bar", BarWidth: 8}, env)
	want := "5h: ●●●●○○○○"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatCountdown_DropsTrailingZeroUnits(t *testing.T) {
	cases := map[int64]string{
		3601:  "1h",   // 1h + 1s -> just 1h (s is below minute granularity)
		3660:  "1h1m", // 1h + 1m + 0s
		86401: "1d",   // 1d + 1s
		90000: "1d1h", // 1d + 1h
		60:    "1m",
		59:    "59s",
		0:     "now",
		-5:    "now",
	}
	for in, want := range cases {
		if got := formatCountdown(in); got != want {
			t.Errorf("formatCountdown(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatPct_AbsorbsFloatingPointGhost(t *testing.T) {
	cases := map[float64]string{
		7.0:               "7%",
		7.5:               "7.5%",
		99.99999999999999: "100%",
		0.0:               "0%",
		100.0:             "100%",
		33.33333333333333: "33.3%",
	}
	for in, want := range cases {
		if got := formatPct(in); got != want {
			t.Errorf("formatPct(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderMode_ThinkingEnabled(t *testing.T) {
	p := &payload{Thinking: thinkF{Enabled: true}}
	if got := renderMode(p, Segment{}, renderEnv{}); got != "🧠" {
		t.Errorf("got %q, want thinking glyph", got)
	}
}

func TestRenderMode_FastMode(t *testing.T) {
	p := &payload{FastMode: true}
	if got := renderMode(p, Segment{}, renderEnv{}); got != "⚡" {
		t.Errorf("got %q, want fast glyph", got)
	}
}

func TestRenderMode_BothFlagsPicksThinking(t *testing.T) {
	p := &payload{FastMode: true, Thinking: thinkF{Enabled: true}}
	if got := renderMode(p, Segment{}, renderEnv{}); got != "🧠" {
		t.Errorf("got %q (thinking should win)", got)
	}
}

func TestRenderMode_NeitherFlagHidesSegment(t *testing.T) {
	if got := renderMode(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestRenderGitBranch_DelegatesToBranchHelper(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/topic\n")

	p := &payload{}
	env := renderEnv{cwd: dir}
	if got := renderGitBranch(p, Segment{}, env); got != "topic" {
		t.Errorf("got %q, want topic", got)
	}
}

func TestRenderGitBranch_EmptyCwdReturnsEmpty(t *testing.T) {
	if got := renderGitBranch(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestRenderGitBranch_LabelPrefix(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	if got := renderGitBranch(&payload{}, Segment{Label: "git"}, renderEnv{cwd: dir}); got != "git: main" {
		t.Errorf("got %q", got)
	}
}

func TestRenderGitBranch_ToplevelScopeFromSubmodule(t *testing.T) {
	sub := submoduleFixture(t, t.TempDir(), "main", "feature-sub")

	env := renderEnv{cwd: sub}
	if got := renderGitBranch(&payload{}, Segment{}, env); got != "feature-sub" {
		t.Errorf("default scope: got %q, want feature-sub", got)
	}
	if got := renderGitBranch(&payload{}, Segment{Scope: "toplevel", Label: "top"}, env); got != "top: main" {
		t.Errorf("toplevel scope: got %q, want \"top: main\"", got)
	}
}

func TestRenderTTYSize_DefaultFormat(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "128×37" {
		t.Errorf("got %q, want %q", got, "128×37")
	}
}

func TestRenderTTYSize_ColsOnlyFormat(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Format: "{cols}c"}, env)
	if got != "128c" {
		t.Errorf("got %q, want %q", got, "128c")
	}
}

func TestRenderTTYSize_LabelPrefix(t *testing.T) {
	env := renderEnv{ttyCols: 128, ttyRows: 37}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Label: "term"}, env)
	if got != "term: 128×37" {
		t.Errorf("got %q, want %q", got, "term: 128×37")
	}
}

func TestRenderTTYSize_HiddenWhenBothZero(t *testing.T) {
	env := renderEnv{ttyCols: 0, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestRenderTTYSize_LabelDoesNotPreventHide(t *testing.T) {
	env := renderEnv{ttyCols: 0, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size", Label: "term"}, env)
	if got != "" {
		t.Errorf("got %q, want \"\" (hide must precede label prefix)", got)
	}
}

func TestRenderVersion_EmptyHidesSegment(t *testing.T) {
	if got := renderVersion(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("empty version should hide segment, got %q", got)
	}
}

func TestRenderVersion_ReleaseVersionPrefixesV(t *testing.T) {
	env := renderEnv{version: "0.2.6"}
	if got := renderVersion(&payload{}, Segment{}, env); got != "v0.2.6" {
		t.Errorf("got %q, want v0.2.6", got)
	}
}

func TestRenderVersion_DevVersionAddsSkullPrefix(t *testing.T) {
	env := renderEnv{version: "dev"}
	if got := renderVersion(&payload{}, Segment{}, env); got != "☠ vdev" {
		t.Errorf("got %q, want ☠ vdev", got)
	}
}

func TestRenderTTYSize_RowsZeroIsNotHidden(t *testing.T) {
	// Config.Width sets cols only; rows stays 0. The segment must still
	// render — "size unknown" is the both-zero case, not rows-zero.
	env := renderEnv{ttyCols: 128, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "128×0" {
		t.Errorf("got %q, want %q", got, "128×0")
	}
}

func TestRenderSchemaHealth_HiddenWhenNoIssue(t *testing.T) {
	if got := renderSchemaHealth(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("got %q, want empty when env.schemaIssue is false", got)
	}
}

func TestRenderSchemaHealth_GlyphWhenIssue(t *testing.T) {
	if got := renderSchemaHealth(&payload{}, Segment{}, renderEnv{schemaIssue: true}); got != "☠" {
		t.Errorf("got %q, want ☠", got)
	}
}

func TestSegmentTypes_SortedAndMatchesRegistry(t *testing.T) {
	types := SegmentTypes()
	if len(types) != len(segmentFuncs) {
		t.Fatalf("SegmentTypes returned %d names, registry has %d", len(types), len(segmentFuncs))
	}
	for i, name := range types {
		if _, ok := segmentFuncs[name]; !ok {
			t.Errorf("SegmentTypes returned %q, which is not in the registry", name)
		}
		if i > 0 && types[i-1] >= name {
			t.Errorf("SegmentTypes not strictly sorted: %q before %q", types[i-1], name)
		}
	}
}

// stubUpdateSpawn replaces the background update-check refresher for the
// duration of a test and reports whether it was triggered. Mirrors
// stubSpawn (gitdirty_test.go) for the update-check refresher.
func stubUpdateSpawn(t *testing.T) *bool {
	t.Helper()
	called := false
	prev := spawnUpdateCheckRefresh
	spawnUpdateCheckRefresh = func() bool { called = true; return true }
	t.Cleanup(func() { spawnUpdateCheckRefresh = prev })
	return &called
}

// withUpdateCache seeds an update-check cache entry and returns the state dir.
func withUpdateCache(t *testing.T, latestTag string, age time.Duration) string {
	t.Helper()
	state := t.TempDir()
	blob, err := json.Marshal(updateCache{LatestTag: latestTag, Unix: nowFunc().Add(-age).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UpdateCachePath(state), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRenderVersion_DevBuildSkipsUpdateCheck(t *testing.T) {
	called := stubUpdateSpawn(t)
	env := renderEnv{version: "dev", stateDir: t.TempDir(), colorEnabled: true}
	if got := renderVersion(nil, Segment{CheckUpdate: true}, env); got != "☠ vdev" {
		t.Errorf("got %q, want \"☠ vdev\"", got)
	}
	if *called {
		t.Error("update check triggered for a dev build")
	}
}

func TestRenderVersion_CheckUpdateDisabledSkipsCheck(t *testing.T) {
	called := stubUpdateSpawn(t)
	state := withUpdateCache(t, "v9.9.9", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true}
	if got := renderVersion(nil, Segment{CheckUpdate: false}, env); got != "v0.4.6" {
		t.Errorf("got %q, want v0.4.6", got)
	}
	if *called {
		t.Error("update check triggered although check_update is false")
	}
}

// TestRenderVersion_UnparsableRunningVersionSkipsNetworkCheck is finding 8
// from the final review: an unparsable running version (e.g. a pseudo
// version) can never be compared against a fetch result, so the network
// trigger must be skipped entirely — not just the render of a result —
// even with no cache present.
func TestRenderVersion_UnparsableRunningVersionSkipsNetworkCheck(t *testing.T) {
	called := stubUpdateSpawn(t)
	env := renderEnv{version: "0.4.6-rc1", stateDir: t.TempDir(), colorEnabled: true}
	if got := renderVersion(nil, Segment{CheckUpdate: true}, env); got != "v0.4.6-rc1" {
		t.Errorf("got %q, want v0.4.6-rc1", got)
	}
	if *called {
		t.Error("update check triggered for an unparsable running version")
	}
}

func TestRenderVersion_NoCacheTriggersRefreshAndShowsPlainVersion(t *testing.T) {
	called := stubUpdateSpawn(t)
	env := renderEnv{version: "0.4.6", stateDir: t.TempDir(), colorEnabled: true}
	if got := renderVersion(nil, Segment{CheckUpdate: true}, env); got != "v0.4.6" {
		t.Errorf("got %q, want v0.4.6", got)
	}
	if !*called {
		t.Error("missing cache did not trigger a refresh")
	}
}

func TestRenderVersion_UpToDateShowsPlainVersion(t *testing.T) {
	called := stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.4.6", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	if got := renderVersion(nil, Segment{CheckUpdate: true}, env); got != "v0.4.6" {
		t.Errorf("got %q, want v0.4.6", got)
	}
	if *called {
		t.Error("refresh triggered although the cache is fresh and up to date")
	}
}

func TestRenderVersion_PatchUpdateUsesAmbientColor(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.4.9", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateMinorFG: "136", UpdateMajorFG: "208", UpdateBigFG: "160"}
	got := renderVersion(nil, s, env)
	want := "v0.4.6 (↑ v0.4.9)" // no ANSI: innerFG (245) == ambientFG (245)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderVersion_MinorUpdateColorsSuffixYellow(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.5.0", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateMinorFG: "136", UpdateMajorFG: "208", UpdateBigFG: "160"}
	got := renderVersion(nil, s, env)
	want := "v0.4.6 (\x1b[38;5;136m↑ v0.5.0\x1b[38;5;245m)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderVersion_MajorUpdateColorsSuffixOrange(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "v1.0.0", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateMinorFG: "136", UpdateMajorFG: "208", UpdateBigFG: "160"}
	got := renderVersion(nil, s, env)
	want := "v0.4.6 (\x1b[38;5;208m↑ v1.0.0\x1b[38;5;245m)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderVersion_FarMajorUpdateColorsSuffixRedWithBolt(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "v2.0.0", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateMinorFG: "136", UpdateMajorFG: "208", UpdateBigFG: "160"}
	got := renderVersion(nil, s, env)
	want := "v0.4.6 (\x1b[38;5;160m⚡ v2.0.0\x1b[38;5;245m)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderVersion_StaleCacheStillRendersAndTriggersRefresh(t *testing.T) {
	called := stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.4.9", 25*time.Hour) // older than the 24h default TTL
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245"}
	if got := renderVersion(nil, s, env); got != "v0.4.6 (↑ v0.4.9)" {
		t.Errorf("got %q, want v0.4.6 (↑ v0.4.9)", got)
	}
	if !*called {
		t.Error("stale cache did not trigger a refresh")
	}
}

func TestRenderVersion_CustomIntervalOverridesDefaultTTL(t *testing.T) {
	called := stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.4.9", 2*time.Hour) // fresh under a 6h interval
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateCheckInterval: "6h"}
	renderVersion(nil, s, env)
	if *called {
		t.Error("refresh triggered although the cache is fresh under the configured interval")
	}
}

func TestRenderVersion_UnparsableCachedTagShowsPlainVersion(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "not-a-version", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: true, nowUnix: nowFunc().Unix()}
	if got := renderVersion(nil, Segment{CheckUpdate: true, FG: "245"}, env); got != "v0.4.6" {
		t.Errorf("got %q, want v0.4.6", got)
	}
}

func TestRenderVersion_NoColorOmitsEscapesButKeepsGlyph(t *testing.T) {
	stubUpdateSpawn(t)
	state := withUpdateCache(t, "v0.5.0", 0)
	env := renderEnv{version: "0.4.6", stateDir: state, colorEnabled: false, nowUnix: nowFunc().Unix()}
	s := Segment{CheckUpdate: true, FG: "245", UpdateMinorFG: "136"}
	if got := renderVersion(nil, s, env); got != "v0.4.6 (↑ v0.5.0)" {
		t.Errorf("got %q, want v0.4.6 (↑ v0.5.0)", got)
	}
}

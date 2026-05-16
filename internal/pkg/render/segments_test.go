package render

import (
	"path/filepath"
	"testing"
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

func TestMakeBar_CircleSequence(t *testing.T) {
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
		if got := makeBar(tc.pct, 4); got != tc.want {
			t.Errorf("makeBar(%.4f, 4) = %q, want %q", tc.pct, got, tc.want)
		}
	}
}

func TestRenderLimit5h_PercentAndCountdown(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 7.0
	p.Limits.FiveHour.ResetsAt = 1_778_412_000

	env := renderEnv{nowUnix: 1_778_412_000 - (4*3600 + 23*60)}
	got := renderLimit5h(p, Segment{}, env)
	if got != "5h: 7% (4h23m)" {
		t.Errorf("got %q, want 5h: 7%% (4h23m)", got)
	}
}

func TestRenderLimit5h_LabelOverride(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 100 - 60} // 1m
	if got := renderLimit5h(p, Segment{Label: "WIN"}, env); got != "WIN: 50% (1m)" {
		t.Errorf("got %q, want WIN: 50%% (1m)", got)
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
	if got := renderLimit7d(p, Segment{}, env); got != "7d: 30% (5d2h)" {
		t.Errorf("got %q, want 7d: 30%% (5d2h)", got)
	}
}

func TestRenderLimit_NegativeRemainingShowsNow(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 10
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 200} // already past
	if got := renderLimit5h(p, Segment{}, env); got != "5h: 10% (now)" {
		t.Errorf("got %q, want 5h: 10%% (now)", got)
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
	want := "5h: ●●●●●●●●○○○○○○○○ 50% (1m)"
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

func TestRenderTTYSize_RowsZeroIsNotHidden(t *testing.T) {
	// Config.Width sets cols only; rows stays 0. The segment must still
	// render — "size unknown" is the both-zero case, not rows-zero.
	env := renderEnv{ttyCols: 128, ttyRows: 0}
	got := renderTTYSize(&payload{}, Segment{Type: "tty_size"}, env)
	if got != "128×0" {
		t.Errorf("got %q, want %q", got, "128×0")
	}
}

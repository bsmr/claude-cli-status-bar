package render

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// segmentFunc renders one segment. It MUST return "" to suppress the
// segment (the row joiner skips empty results). It MAY emit ANSI
// escape sequences for sub-region styling (e.g. threshold coloring
// of a percentage substring via wrapPct); when it does, it MUST
// close every opened sequence before returning so the renderer's
// outer style() wrap is not broken.
type segmentFunc func(p *payload, s Segment, env renderEnv) string

// segmentFuncs is the type registry. The bare `text` segment lives here as
// the registry's only literal entry; richer segments register themselves
// in init().
var segmentFuncs = map[string]segmentFunc{
	"text": renderText,
}

func init() {
	segmentFuncs["model"] = renderModel
	segmentFuncs["effort"] = renderEffort
	segmentFuncs["session_name"] = renderSessionName
	segmentFuncs["output_style"] = renderOutputStyle
	segmentFuncs["cwd"] = renderCwd
	segmentFuncs["cost"] = renderCost
	segmentFuncs["duration"] = renderDuration
	segmentFuncs["lines"] = renderLines
	segmentFuncs["context"] = renderContext
	segmentFuncs["limit_5h"] = renderLimit5h
	segmentFuncs["limit_7d"] = renderLimit7d
	segmentFuncs["mode"] = renderMode
	segmentFuncs["git_branch"] = renderGitBranch
}

// renderText returns the segment's Label verbatim. Useful as a literal
// separator or to inject custom strings.
func renderText(_ *payload, s Segment, _ renderEnv) string {
	return s.Label
}

// renderModel returns the model display name. When Show1MFlag is true and
// the payload's exceeds_200k_tokens flag is set, "1M" is appended; any
// existing parenthetical suffix in the display name is dropped first to
// avoid double-flagging like "Opus 4.7 (1M context) 1M".
func renderModel(p *payload, s Segment, _ renderEnv) string {
	name := p.Model.DisplayName
	if i := strings.Index(name, " ("); i > 0 {
		name = name[:i]
	}
	if s.Show1MFlag && p.Exceeds200kT {
		name += " 1M"
	}
	return name
}

// renderEffort returns "<label>:<level>" when effort.level is non-empty.
// Default label is "effort"; override via Segment.Label.
func renderEffort(p *payload, s Segment, _ renderEnv) string {
	if p.Effort.Level == "" {
		return ""
	}
	label := s.Label
	if label == "" {
		label = "effort"
	}
	return label + ":" + p.Effort.Level
}

// renderSessionName returns session_name verbatim.
func renderSessionName(p *payload, _ Segment, _ renderEnv) string {
	return p.SessionName
}

// renderOutputStyle returns "<label>:<name>" when output_style.name is
// non-empty. Default label is "style".
func renderOutputStyle(p *payload, s Segment, _ renderEnv) string {
	if p.OutputStyle.Name == "" {
		return ""
	}
	label := s.Label
	if label == "" {
		label = "style"
	}
	return label + ":" + p.OutputStyle.Name
}

// renderCwd returns the basename of workspace.current_dir by default.
// Set Segment.Format = "full" to emit the absolute path instead.
func renderCwd(p *payload, s Segment, _ renderEnv) string {
	dir := p.Workspace.CurrentDir
	if dir == "" {
		return ""
	}
	if s.Format == "full" {
		return dir
	}
	return filepath.Base(dir)
}

// renderCost returns the total cost using fmt verb format. Default is
// "$%.2f"; override via Segment.Format. Zero is rendered explicitly
// rather than hidden, since "$0.00" is meaningful early in a session.
func renderCost(p *payload, s Segment, _ renderEnv) string {
	format := s.Format
	if format == "" {
		format = "$%.2f"
	}
	return fmt.Sprintf(format, p.Cost.TotalCostUSD)
}

// renderDuration formats total_duration_ms as a compact h/m/s string,
// dropping zero-valued higher units. "2h10m", "5m", "12s". Hidden when
// the field is zero or negative.
func renderDuration(p *payload, _ Segment, _ renderEnv) string {
	ms := p.Cost.TotalDurationMS
	if ms <= 0 {
		return ""
	}
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// renderLines emits "+<added> <U+2212 minus><removed>" when at least one
// counter is non-zero. Hidden when both are zero.
func renderLines(p *payload, _ Segment, _ renderEnv) string {
	a, r := p.Cost.TotalLinesAdded, p.Cost.TotalLinesRemoved
	if a == 0 && r == 0 {
		return ""
	}
	return fmt.Sprintf("+%d −%d", a, r)
}

const barCells = 16

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

// makeBar renders a unicode block-element bar of length cells, filled
// proportionally to pct (0-100, clamped to that range).
func makeBar(pct float64, cells int) string {
	filled := int(pct * float64(cells) / 100)
	filled = min(max(filled, 0), cells)
	var b strings.Builder
	b.WriteByte('[')
	for range filled {
		b.WriteString("█") // U+2588 FULL BLOCK
	}
	for range cells - filled {
		b.WriteString("░") // U+2591 LIGHT SHADE
	}
	b.WriteByte(']')
	return b.String()
}

// formatTokens compacts large numbers: 1234 → "1k", 273000 → "273k",
// 1000000 → "1M", 1_500_000 → "1.5M".
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		if n%1_000_000 == 0 {
			return fmt.Sprintf("%dM", n/1_000_000)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// renderLimit5h emits the five-hour rate-limit bucket as
// "<label>:<pct>% (<countdown>)". Default label "5h".
func renderLimit5h(p *payload, s Segment, env renderEnv) string {
	return renderLimit(p.Limits.FiveHour, p, s, env, "5h")
}

// renderLimit7d emits the seven-day rate-limit bucket. Default label "7d".
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

// formatPct returns "7%" for 7.0 and "7.5%" for non-integer values.
// math.Round absorbs tiny floating-point artifacts so 99.99999...
// renders as "100%" rather than "100.0%".
func formatPct(p float64) string {
	if r := math.Round(p); math.Abs(p-r) < 0.0005 {
		return fmt.Sprintf("%d%%", int(r))
	}
	return fmt.Sprintf("%.1f%%", p)
}

// formatCountdown returns a compact string for a future delta in seconds.
// Higher units are shown first; the second unit is included only when it's
// non-zero. Negative or zero -> "now".
func formatCountdown(secs int64) string {
	if secs <= 0 {
		return "now"
	}
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	switch {
	case d > 0:
		if h > 0 {
			return fmt.Sprintf("%dd%dh", d, h)
		}
		return fmt.Sprintf("%dd", d)
	case h > 0:
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// renderMode returns a glyph indicating the inference mode: 🧠 when
// thinking is enabled, ⚡ when fast_mode is set, otherwise empty. When
// both flags are set, thinking wins because it's the slower, more
// noteworthy state.
func renderMode(p *payload, _ Segment, _ renderEnv) string {
	switch {
	case p.Thinking.Enabled:
		return "\U0001F9E0" // 🧠 BRAIN
	case p.FastMode:
		return "⚡" // ⚡ HIGH VOLTAGE SIGN
	default:
		return ""
	}
}

// renderGitBranch reads the branch name from env.cwd/.git/HEAD. With a
// non-empty Segment.Label, the result is prefixed as "<label>:<branch>".
// Returns "" when not in a git repo, detached HEAD, or env.cwd is empty.
func renderGitBranch(_ *payload, s Segment, env renderEnv) string {
	b := branch(env.cwd)
	if b == "" {
		return ""
	}
	if s.Label == "" {
		return b
	}
	return s.Label + ":" + b
}

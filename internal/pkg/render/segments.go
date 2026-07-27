package render

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
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
	segmentFuncs["git_dirty"] = renderGitDirty
	segmentFuncs["tty_size"] = renderTTYSize
	segmentFuncs["version"] = renderVersion
	segmentFuncs["schema_health"] = renderSchemaHealth
}

// SegmentTypes returns the sorted names of every registered segment type.
// Exported so consumers that carry their own segment vocabulary — notably the
// ccsb-wizard skill asset — can be tested against what the renderer actually
// implements, instead of drifting into names that do not exist.
func SegmentTypes() []string {
	out := make([]string, 0, len(segmentFuncs))
	for name := range segmentFuncs {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
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
	if before, _, found := strings.Cut(name, " ("); found && before != "" {
		name = before
	}
	if s.Show1MFlag && p.Exceeds200kT {
		name += " 1M"
	}
	return name
}

// renderEffort returns "<label>: <level>" when effort.level is non-empty.
// Default label is "effort"; override via Segment.Label.
func renderEffort(p *payload, s Segment, _ renderEnv) string {
	if p.Effort.Level == "" {
		return ""
	}
	label := s.Label
	if label == "" {
		label = "effort"
	}
	return label + ": " + p.Effort.Level
}

// renderSessionName returns session_name verbatim.
func renderSessionName(p *payload, _ Segment, _ renderEnv) string {
	return p.SessionName
}

// renderOutputStyle returns "<label>: <name>" when output_style.name is
// non-empty. Default label is "style".
func renderOutputStyle(p *payload, s Segment, _ renderEnv) string {
	if p.OutputStyle.Name == "" {
		return ""
	}
	label := s.Label
	if label == "" {
		label = "style"
	}
	return label + ": " + p.OutputStyle.Name
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

// effectiveBarCells returns the circle-bar length for a segment:
// Segment.BarWidth when positive, otherwise the package default
// barCells. Zero or negative values fall back to the default so a
// missing or nonsensical config cannot collapse the bar to nothing.
func effectiveBarCells(s Segment) int {
	if s.BarWidth > 0 {
		return s.BarWidth
	}
	return barCells
}

// renderContext draws the context-window state. Style "bar+pct" (default)
// emits the unicode block bar, the rounded percent, and used/total token
// counts. "bar" or "pct" omit the other parts. Hidden when both
// UsedPercentage and ContextWindowSize are zero (no data). When
// Segment.ThresholdTarget=="pct" the percent digits are wrapped in the
// threshold FG via wrapPct so the bar and tokens stay in the static FG.
// Segment.TokenPosition places the token fraction in the "bar+pct" style:
// "after" (default) trails the percent, "before" leads the bar, "hidden"
// omits it.
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
	amb := ambientFG(s, p)
	bar := wrapPart(makeBarGlyphs(p.Context.UsedPercentage, effectiveBarCells(s), barRamp(s)),
		pickThresholdFG(s.BarFG, s.BarThresholds, s.Type, p), amb, env.colorEnabled)
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
		tokens := formatTokens(used) + "/" + formatTokens(p.Context.ContextWindowSize)
		switch s.TokenPosition {
		case "hidden":
			return fmt.Sprintf("%s %s", bar, pctStyled)
		case "before":
			return fmt.Sprintf("%s %s %s", tokens, bar, pctStyled)
		default: // "after", "" or unknown
			return fmt.Sprintf("%s %s %s", bar, pctStyled, tokens)
		}
	}
}

// circleSteps maps quarter-fill level (0–4) to a Unicode circle glyph.
var circleSteps = []string{
	"○", // ○ U+25CB WHITE CIRCLE            (0/4)
	"◔", // ◔ U+25D4 UPPER-RIGHT QUADRANT    (1/4)
	"◑", // ◑ U+25D1 RIGHT HALF BLACK         (2/4)
	"◕", // ◕ U+25D5 ALL BUT UPPER-LEFT       (3/4)
	"●", // ● U+25CF BLACK CIRCLE             (4/4)
}

// blockSteps is the "blocks" preset ramp: a light-shade empty, seven
// left-anchored eighth blocks, and a full block.
var blockSteps = []string{"░", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// barRamp resolves the fill ramp for a bar-drawing segment. An explicit
// BarGlyphs (>= 2 entries) wins; otherwise BarStyle selects a preset
// ("blocks" -> blockSteps), defaulting to circleSteps for "circles", "",
// or any unknown value.
func barRamp(s Segment) []string {
	if len(s.BarGlyphs) >= 2 {
		return s.BarGlyphs
	}
	if s.BarStyle == "blocks" {
		return blockSteps
	}
	return circleSteps
}

// makeBarGlyphs renders a bar of length cells using ramp, an ordered fill
// sequence from empty (index 0) to full (last index). Each cell resolves to
// one of len(ramp)-1 sub-steps, yielding cells×(len-1) discrete levels. A
// ramp shorter than two entries falls back to circleSteps so a malformed
// config cannot panic or blank the bar.
func makeBarGlyphs(pct float64, cells int, ramp []string) string {
	if len(ramp) < 2 {
		ramp = circleSteps
	}
	sub := len(ramp) - 1
	total := cells * sub
	filled := int(pct*float64(total)/100 + 0.5)
	filled = min(max(filled, 0), total)
	fullCells := filled / sub
	remainder := filled % sub
	var b strings.Builder
	for i := range cells {
		switch {
		case i < fullCells:
			b.WriteString(ramp[sub])
		case i == fullCells && remainder > 0:
			b.WriteString(ramp[remainder])
		default:
			b.WriteString(ramp[0])
		}
	}
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
// "<label>: <pct>% (<countdown>)". Default label "5h".
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
//   - "" or "pct" (default): "<label>: <pct> · <countdown>"
//   - "bar":                 "<label>: <bar>"
//   - "bar+pct":             "<label>: <bar> <pct> · <countdown>"
func renderLimit(rl rateLimitF, p *payload, s Segment, env renderEnv, defaultLabel string) string {
	if rl.UsedPercentage == 0 && rl.ResetsAt == 0 {
		return ""
	}
	label := s.Label
	if label == "" {
		label = defaultLabel
	}
	pct := wrapPct(formatPct(rl.UsedPercentage), s, p, env.colorEnabled)
	cells := effectiveBarCells(s)
	ramp := barRamp(s)
	amb := ambientFG(s, p)
	labeled := wrapPart(label+":", pickThresholdFG(s.LabelFG, s.LabelThresholds, s.Type, p), amb, env.colorEnabled)
	bar := wrapPart(makeBarGlyphs(rl.UsedPercentage, cells, ramp), pickThresholdFG(s.BarFG, s.BarThresholds, s.Type, p), amb, env.colorEnabled)

	switch s.Style {
	case "bar":
		return labeled + " " + bar
	case "bar+pct":
		if rl.ResetsAt == 0 {
			return labeled + " " + bar + " " + pct
		}
		return labeled + " " + bar + " " + pct + " · " + formatCountdown(rl.ResetsAt-env.nowUnix)
	default: // "" or "pct"
		if rl.ResetsAt == 0 {
			return labeled + " " + pct
		}
		return labeled + " " + pct + " · " + formatCountdown(rl.ResetsAt-env.nowUnix)
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

// renderGitBranch reads the branch name from env.cwd/.git/HEAD. Segment.Scope
// selects the submodule-aware repository ("local" default, or "toplevel" for
// the outermost superproject). With a non-empty Segment.Label, the result is
// prefixed as "<label>: <branch>". Returns "" when not in a git repo, detached
// HEAD, or env.cwd is empty.
func renderGitBranch(_ *payload, s Segment, env renderEnv) string {
	b := branchScoped(env.cwd, s.Scope)
	if b == "" {
		return ""
	}
	if s.Label == "" {
		return b
	}
	return s.Label + ": " + b
}

// renderGitDirty returns the number of paths git reports as changed in the
// repository containing env.cwd — modified, staged, deleted, renamed, and
// untracked alike.
//
// The count is served from a cache and refreshed OUT OF BAND: this function
// never runs git. When the cached value is older than dirtyCacheTTL (or
// absent) it starts a detached `ccsb refresh-git-dirty` in the background
// and renders whatever it has right now, so a slow repository can never
// delay the status line — the fresher number simply appears one update
// later.
//
// Segment.Format supports the {n} placeholder and defaults to "*{n}"; a
// non-empty Segment.Label is prefixed as "<label>: <body>". Returns ""
// for a clean tree, an unavailable count, or outside a repository, so the
// segment (and its separator) drops out entirely.
func renderGitDirty(_ *payload, s Segment, env renderEnv) string {
	if env.stateDir == "" {
		return ""
	}
	gitDir, ok := nearestGitDir(env.cwd)
	if !ok {
		return ""
	}

	cached, found := readDirtyCache(DirtyCachePath(env.stateDir, gitDir))
	// A negative age means the entry is stamped in the future — a backward
	// clock step, an NTP correction, or a cache copied from another host.
	// Without the lower bound such an entry would never reach the TTL and
	// would be served until wall-clock time caught up; treating it as stale
	// refreshes it, and the refresher's write re-stamps it with this host's
	// clock, so the entry self-heals in one cycle.
	age := env.nowUnix - cached.Unix
	if !found || age < 0 || age >= int64(dirtyCacheTTL.Seconds()) {
		// Single-flight: only the caller that wins the lock spawns a
		// refresher. This function is invoked several times per render (the
		// layout engine's measurement passes) and by every parallel session
		// on the repo — without the guard each of those would fork its own
		// `git status`. The lock coordinates across processes via O_EXCL.
		if acquireRefreshLock(env.stateDir, gitDir) {
			if !spawnDirtyRefresh(env.cwd) {
				// The child never started, so nothing will release the marker;
				// drop it now so the next render retries instead of waiting
				// out the lock TTL.
				releaseRefreshLock(env.stateDir, gitDir)
			}
		}
	}
	if !found || cached.Count <= 0 {
		return ""
	}

	format := s.Format
	if format == "" {
		format = "*{n}"
	}
	out := strings.ReplaceAll(format, "{n}", strconv.Itoa(cached.Count))
	if s.Label != "" {
		out = s.Label + ": " + out
	}
	return out
}

// renderTTYSize formats env.ttyCols × env.ttyRows. Returns "" when
// detection failed (both dimensions 0), so the empty-segment-drop path
// in both renderers omits the segment plus its surrounding separator
// or chevron. s.Format supports the placeholders {cols} and {rows};
// the default format is "{cols}×{rows}" using U+00D7 (multiplication
// sign), not ASCII 'x'. s.Label, when set, prefixes "<label>: ".
func renderTTYSize(_ *payload, s Segment, env renderEnv) string {
	if env.ttyCols == 0 && env.ttyRows == 0 {
		return ""
	}
	format := s.Format
	if format == "" {
		format = "{cols}×{rows}"
	}
	out := strings.ReplaceAll(format, "{cols}", strconv.Itoa(env.ttyCols))
	out = strings.ReplaceAll(out, "{rows}", strconv.Itoa(env.ttyRows))
	if s.Label != "" {
		out = s.Label + ": " + out
	}
	return out
}

// renderVersion emits the ccsb version string from env.version, plus an
// optional "(<glyph> v<latest>)" suffix when a newer GitHub release is
// available. Hidden when env.version is empty. Dev builds (version ==
// "dev") are prefixed with ☠ (U+2620 SKULL AND CROSSBONES) and never check
// for updates — there is no tagged version to compare against.
//
// The update check itself never runs on this call: it only ever reads
// env.stateDir's cache (see updatecheck.go) and, when that cache is
// missing or stale, spawns a detached background refresher. The render
// always returns immediately with whatever the cache currently holds.
func renderVersion(_ *payload, s Segment, env renderEnv) string {
	if env.version == "" {
		return ""
	}
	if env.version == "dev" {
		return "☠ v" + env.version // ☠ vdev
	}
	base := "v" + env.version
	if !s.CheckUpdate || env.stateDir == "" {
		return base
	}
	suffix := renderUpdateSuffix(s, env)
	if suffix == "" {
		return base
	}
	return base + " (" + suffix + ")"
}

// buildInfoFunc reads this binary's build info; goosFunc reports the target
// OS. Package variables so tests can drive updateBlocked: a test binary is
// always a local build, so the real ReadBuildInfo would report every test as
// blocked. Production code never reassigns them.
var (
	buildInfoFunc = debug.ReadBuildInfo
	goosFunc      = func() string { return runtime.GOOS }
)

// renderUpdateSuffix returns the colored "<glyph> v<latest>" fragment for
// renderVersion's parenthesized suffix, or "" when there is nothing to
// show (running version unparsable, no cache yet, cached tag unparsable,
// or latest is not newer). Triggers a background refresh via
// spawnUpdateCheckRefresh whenever the cache is missing, stale, or
// clock-skewed into the future — mirroring renderGitDirty's out-of-band
// refresh trigger. The running-version check runs first and skips the
// network trigger entirely when it fails: an unparsable running version
// (e.g. a pseudo-version) can never be compared against a fetch result,
// so there is no point spawning a refresher for it every TTL interval.
func renderUpdateSuffix(s Segment, env renderEnv) string {
	current, ok := ParseSemver(env.version)
	if !ok {
		return ""
	}

	cached, found := readUpdateCache(UpdateCachePath(env.stateDir))
	ttl := parseUpdateCheckInterval(s.UpdateCheckInterval)
	age := env.nowUnix - cached.Unix
	if !found || age < 0 || age >= int64(ttl.Seconds()) {
		lockPath := updateLockPath(env.stateDir)
		if acquireLock(lockPath, updateCheckLockTTL) {
			if !spawnUpdateCheckRefresh() {
				// The child never started, so nothing will release the
				// marker; drop it now so the next render retries instead
				// of waiting out the lock TTL.
				releaseLock(lockPath)
			}
		}
	}
	if !found {
		return ""
	}
	latest, ok := ParseSemver(cached.LatestTag)
	if !ok {
		return ""
	}
	// Compute severity and bail out before touching updateBlocked: Go
	// evaluates call arguments eagerly, so inlining updateBlocked(...) into
	// the updateSeverityStyle call below would read update-attempt.json on
	// every render, even for the overwhelmingly common SeverityNone case
	// (already up to date) where the result is immediately discarded. Do
	// not collapse this back into a single expression.
	sev := CompareSeverity(current, latest)
	if sev == SeverityNone {
		return ""
	}
	glyph, fg := updateSeverityStyle(s, sev, updateBlocked(env.stateDir))
	text := fmt.Sprintf("%s v%d.%d.%d", glyph, latest.Major, latest.Minor, latest.Patch)
	return wrapPart(text, fg, s.FG, env.colorEnabled)
}

// updateBlockedGlyph marks a detected update that cannot be applied on this
// system — the user has to intervene by hand. Distinct from the ↑/⚡ glyphs
// so it can be looked up in the documentation and in `ccsb doctor`.
const updateBlockedGlyph = "⊘"

// updateBlocked reports whether a self-update is impossible here. Probes in
// cost order and never touches the network or tests filesystem permissions:
// the two in-process checks answer immediately, and the attempt record is a
// file this package reads anyway.
//
// Consequence worth knowing: the not-writable case only surfaces after an
// update has actually been attempted once. Detecting it earlier would mean
// probing the filesystem during a render, which this project does not do.
func updateBlocked(stateDir string) bool {
	if goosFunc() == "windows" {
		return true
	}
	if info, ok := buildInfoFunc(); ok && info != nil {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return true
			}
		}
	}
	att, ok := ReadUpdateAttempt(stateDir)
	return ok && att.Blocked != BlockNone
}

// updateSeverityStyle maps a Severity to its glyph and foreground color.
// SeverityNone returns ("", "") — the caller treats an empty glyph as
// "nothing to render". A patch-level update uses s.FG as its color (the
// segment's own ambient color), so wrapPart renders it with no visible
// recolor — only minor/major/majorFar updates stand out.
//
// When blocked is true the glyph becomes ⊘ while the severity's color is
// kept: escalation by jump distance still applies, only the message changes
// from "go get it" to "go get it by hand".
func updateSeverityStyle(s Segment, sev Severity, blocked bool) (glyph, fg string) {
	switch sev {
	case SeverityPatch:
		glyph, fg = "↑", s.FG
	case SeverityMinor:
		glyph, fg = "↑", s.UpdateMinorFG
	case SeverityMajor:
		glyph, fg = "↑", s.UpdateMajorFG
	case SeverityMajorFar:
		glyph, fg = "⚡", s.UpdateBigFG
	default:
		return "", ""
	}
	if blocked {
		glyph = updateBlockedGlyph
	}
	return glyph, fg
}

// spawnUpdateCheckRefresh re-executes ccsb as `ccsb refresh-update-check`
// and returns immediately without waiting, mirroring spawnDirtyRefresh in
// gitdirty.go. Every failure is silent by design: a refresh that cannot
// start simply leaves the cached value in place for the next render to
// retry. Overridable so tests can observe the trigger without starting
// processes.
var spawnUpdateCheckRefresh = func() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	cmd := exec.Command(self, "refresh-update-check")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return false
	}
	_ = cmd.Process.Release()
	return true
}

// renderSchemaHealth emits a single skull glyph (☠ U+2620) when
// env.schemaIssue is true, signalling that the inbound JSON payload from
// Claude Code looks broken (parse failed, or a critical field is empty).
// Returns "" otherwise so the segment is hidden and skips its palette
// slot. The visible glyph is plain text — the alarm colour comes from
// the segment's FG/BG/Bold via the outer style() wrap, so the default
// config can paint it red-on-darkred while a user override can recolour
// or restyle it freely.
func renderSchemaHealth(_ *payload, _ Segment, env renderEnv) string {
	if !env.schemaIssue {
		return ""
	}
	return "☠"
}

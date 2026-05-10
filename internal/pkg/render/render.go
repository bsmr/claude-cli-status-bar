// Package render builds the configurable statusLine output for ccsb.
//
// The public surface is Config, Segment, Options, and Render. Per-segment
// renderers are registered in segments.go; ANSI helpers live in ansi.go;
// .git/HEAD branch lookup in git.go.
package render

import (
	"encoding/json"
	"strings"
	"time"
)

// Config is the on-disk render schema, embedded into config.Config.
type Config struct {
	Rows      [][]Segment `json:"rows,omitzero"`
	Separator string      `json:"separator,omitempty"`
}

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
}

// Options configures a single Render call.
type Options struct {
	Config Config
	// Cwd, when non-empty, overrides payload.workspace.current_dir as the
	// starting directory for git-state lookup.
	Cwd string
	// NoColor disables ANSI escape emission. Resolved by the caller from
	// the NO_COLOR environment variable (no-color.org convention).
	NoColor bool
}

// payload mirrors the subset of the Claude Code statusLine payload that
// segments consume. Unknown fields are ignored.
//
// Field-shape helper structs use the F suffix (modelF, costF, ...) so they
// stay grep-distinct from any future public domain types and are clearly
// JSON-shape carriers, not domain models.
type payload struct {
	SessionID    string    `json:"session_id"`
	SessionName  string    `json:"session_name"`
	Model        modelF    `json:"model"`
	Workspace    workspace `json:"workspace"`
	OutputStyle  outputF   `json:"output_style"`
	Effort       effortF   `json:"effort"`
	Cost         costF     `json:"cost"`
	Context      contextF  `json:"context_window"`
	Limits       limitsF   `json:"rate_limits"`
	FastMode     bool      `json:"fast_mode"`
	Thinking     thinkF    `json:"thinking"`
	Exceeds200kT bool      `json:"exceeds_200k_tokens"`
}

type modelF struct {
	DisplayName string `json:"display_name"`
}
type workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}
type outputF struct {
	Name string `json:"name"`
}
type effortF struct {
	Level string `json:"level"`
}
type costF struct {
	TotalCostUSD      float64 `json:"total_cost_usd"`
	TotalDurationMS   int64   `json:"total_duration_ms"`
	TotalLinesAdded   int64   `json:"total_lines_added"`
	TotalLinesRemoved int64   `json:"total_lines_removed"`
}
type contextF struct {
	UsedPercentage    float64 `json:"used_percentage"`
	ContextWindowSize int64   `json:"context_window_size"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	CurrentUsage      struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"current_usage"`
}
type limitsF struct {
	FiveHour rateLimitF `json:"five_hour"`
	SevenDay rateLimitF `json:"seven_day"`
}
type rateLimitF struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}
type thinkF struct {
	Enabled bool `json:"enabled"`
}

// nowFunc returns the current time. Tests swap this for a fixed clock so
// countdown formatting is deterministic.
var nowFunc = time.Now

// defaultRows is used when Config.Rows is empty.
var defaultRows = [][]Segment{
	{{Type: "model", Show1MFlag: true}, {Type: "context", Style: "bar+pct"}},
	{{Type: "cost"}, {Type: "limit_5h"}, {Type: "limit_7d"}},
	{{Type: "git_branch"}, {Type: "cwd"}},
}

const defaultSeparator = " | "

// Render parses raw, walks Config.Rows, and returns the joined multi-line
// output. An empty Config.Rows triggers defaultRows. A global JSON-parse
// failure falls back to a hardcoded "<model> · <cwd>" so Claude Code never
// gets an empty statusLine.
func Render(opts Options, raw []byte) (string, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		// last-resort single-line so the bar is never blank.
		return lastResort(opts, raw), nil
	}

	rows := opts.Config.Rows
	if len(rows) == 0 {
		rows = defaultRows
	}
	sep := opts.Config.Separator
	if sep == "" {
		sep = defaultSeparator
	}

	cwd := opts.Cwd
	if cwd == "" {
		cwd = p.Workspace.CurrentDir
	}

	env := renderEnv{
		cwd:          cwd,
		colorEnabled: !opts.NoColor,
		nowUnix:      nowFunc().Unix(),
	}

	var lines []string
	for _, row := range rows {
		var parts []string
		for _, seg := range row {
			s := renderSegment(&p, seg, env)
			if s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, sep))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// renderEnv carries per-call state from Render to each segment renderer.
// Segment functions read these fields but never mutate them.
type renderEnv struct {
	cwd          string // resolved cwd (Options.Cwd or payload.Workspace.CurrentDir)
	colorEnabled bool   // false when NoColor was set on Options
	nowUnix      int64  // wall clock at the start of Render, for time-based segments
}

// renderSegment dispatches one segment via the registry. Unknown types
// render "?<type>?" so typos are visible without breaking the row.
func renderSegment(p *payload, s Segment, env renderEnv) string {
	fn, ok := segmentFuncs[s.Type]
	if !ok {
		return "?" + s.Type + "?"
	}
	out := fn(p, s, env)
	return style(out, s.FG, s.BG, s.Bold, env.colorEnabled)
}

// lastResort renders a single line from whatever fields we can salvage when
// the payload is unparsable.
func lastResort(opts Options, raw []byte) string {
	// Try a relaxed second pass: pick model and cwd if they happen to be
	// present as flat strings somewhere. If not, fall back to a literal.
	var loose struct {
		Model     modelF    `json:"model"`
		Workspace workspace `json:"workspace"`
	}
	_ = json.Unmarshal(raw, &loose)
	model := loose.Model.DisplayName
	cwd := opts.Cwd
	if cwd == "" {
		cwd = loose.Workspace.CurrentDir
	}
	switch {
	case model != "" && cwd != "":
		return model + " · " + cwd
	case model != "":
		return model
	case cwd != "":
		return cwd
	default:
		return "claude-cli-status-bar"
	}
}

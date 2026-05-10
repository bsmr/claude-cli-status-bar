# Native Renderer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a configurable Go-native statusLine renderer in a new `internal/pkg/render` package that replaces the trivial `<model> · <dir>` fallback and handles every payload field that ccstatusline ignores (cost, both rate limits, 1M flag, effort, session name).

**Architecture:** New package `render` with sub-files for ANSI, git, segments, and the public `Render` entry point. Segments dispatched via `map[string]segmentFunc`. `config.Config` gains a `Render render.Config` field; `statusline.Run` calls `render.Render` whenever no proxy command is configured. CLI gets a new `Flags` struct (separate from `Paths`) carrying env-derived booleans like `NoColor`.

**Tech Stack:** Go 1.26, stdlib only. ANSI 256-color codes. Table-driven tests. Golden tests for the integration layer using anonymised real captures from this session.

**Source-of-truth spec:** `docs/superpowers/specs/2026-05-10-native-renderer-design.md` — read it before starting any task.

---

## File Structure

```
internal/pkg/render/                          (new package)
├── render.go                                  // Config, Segment, Options, Payload, Render(), defaultRows, dispatcher
├── render_test.go
├── segments.go                                // segment registry + per-type renderers
├── segments_test.go
├── ansi.go                                    // fg256, bg256, style, reset
├── ansi_test.go
├── git.go                                     // branch(start) string + helpers
├── git_test.go
└── testdata/
    ├── payloads/                              // anonymised captures
    │   ├── low_cost.json
    │   ├── high_cost_1m.json
    │   ├── near_5h_limit.json
    │   ├── after_5h_reset.json
    │   └── detached_head.json
    └── golden/                                // expected default-render output per payload
        ├── low_cost.txt
        ├── high_cost_1m.txt
        ├── near_5h_limit.txt
        ├── after_5h_reset.txt
        └── detached_head.txt

internal/pkg/config/config.go                  (modified: add Render render.Config field)
internal/pkg/statusline/statusline.go          (modified: call render.Render when no proxy)
internal/pkg/statusline/statusline_test.go     (modified: cover Render path)
internal/pkg/cli/cli.go                        (modified: add Flags struct, plumb to statusline.Options)
internal/pkg/cli/cli_test.go                   (modified: pass Flags{} in tests)
cmd/ccsb/main.go                               (modified: resolve NO_COLOR, pass Flags)
```

---

## Task 1: ANSI helpers

**Files:**
- Create: `internal/pkg/render/ansi.go`
- Create: `internal/pkg/render/ansi_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/pkg/render/ansi_test.go`:

```go
package render

import "testing"

func TestFg256_ValidNumberReturnsEscapeSequence(t *testing.T) {
	if got := fg256("131"); got != "\x1b[38;5;131m" {
		t.Errorf("got %q, want \\x1b[38;5;131m", got)
	}
	if got := fg256("0"); got != "\x1b[38;5;0m" {
		t.Errorf("got %q for 0", got)
	}
	if got := fg256("255"); got != "\x1b[38;5;255m" {
		t.Errorf("got %q for 255", got)
	}
}

func TestFg256_RejectsInvalidInput(t *testing.T) {
	for _, in := range []string{"", "256", "300", "-1", "abc", "12;5", "1\x1b"} {
		if got := fg256(in); got != "" {
			t.Errorf("fg256(%q) should reject, got %q", in, got)
		}
	}
}

func TestBg256_ValidNumberReturnsEscapeSequence(t *testing.T) {
	if got := bg256("220"); got != "\x1b[48;5;220m" {
		t.Errorf("got %q, want \\x1b[48;5;220m", got)
	}
}

func TestStyle_NoColorReturnsRawText(t *testing.T) {
	if got := style("hi", "131", "220", true, false); got != "hi" {
		t.Errorf("with colorEnabled=false expected raw text, got %q", got)
	}
}

func TestStyle_AppliesAllAttributes(t *testing.T) {
	got := style("hi", "131", "220", true, true)
	want := "\x1b[1m\x1b[38;5;131m\x1b[48;5;220mhi\x1b[0m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStyle_OmitsEmptyAttributes(t *testing.T) {
	if got := style("hi", "", "", false, true); got != "hi\x1b[0m" {
		t.Errorf("with no attrs expected reset only, got %q", got)
	}
	if got := style("hi", "131", "", false, true); got != "\x1b[38;5;131mhi\x1b[0m" {
		t.Errorf("fg only: got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/...
```

Expected: build failure (`fg256`, `bg256`, `style` undefined). The package itself doesn't compile yet.

- [ ] **Step 3: Implement ansi.go**

Create `internal/pkg/render/ansi.go`:

```go
// Package render builds the configurable statusLine output for ccsb.
//
// This file holds ANSI 256-color helpers. Public types and the Render entry
// point live in render.go; per-segment renderers in segments.go.
package render

import "strings"

const reset = "\x1b[0m"

// fg256 returns the ANSI 256-color foreground escape for n, or "" if n is
// not a valid 0-255 decimal string. Validation is strict to prevent escape
// sequence injection from a malicious config file.
func fg256(n string) string {
	if !validColor(n) {
		return ""
	}
	return "\x1b[38;5;" + n + "m"
}

// bg256 is the background variant of fg256.
func bg256(n string) string {
	if !validColor(n) {
		return ""
	}
	return "\x1b[48;5;" + n + "m"
}

// style wraps s in optional bold + foreground + background escapes,
// terminated by a reset. When colorEnabled is false, s is returned verbatim.
func style(s, fg, bg string, bold, colorEnabled bool) string {
	if !colorEnabled {
		return s
	}
	var b strings.Builder
	if bold {
		b.WriteString("\x1b[1m")
	}
	b.WriteString(fg256(fg))
	b.WriteString(bg256(bg))
	b.WriteString(s)
	b.WriteString(reset)
	return b.String()
}

func validColor(n string) bool {
	if n == "" || len(n) > 3 {
		return false
	}
	v := 0
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
		v = v*10 + int(r-'0')
	}
	return v <= 255
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pkg/render/...
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/
git commit -s -m "$(cat <<'EOF'
feat(render): ANSI 256-color helpers with strict validation

Adds the render package skeleton with fg256/bg256/style/reset. Color
strings are validated as decimal 0-255 only so malicious config values
cannot inject arbitrary escape sequences.
EOF
)"
```

---

## Task 2: Git branch lookup

**Files:**
- Create: `internal/pkg/render/git.go`
- Create: `internal/pkg/render/git_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/pkg/render/git_test.go`:

```go
package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranch_RegularRepoOnMain(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	if got := branch(dir); got != "main" {
		t.Errorf("got %q, want main", got)
	}
}

func TestBranch_FromSubdirectoryWalksUp(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/feature-x\n")
	sub := filepath.Join(dir, "a", "b", "c")
	mustMkdir(t, sub)

	if got := branch(sub); got != "feature-x" {
		t.Errorf("from sub: got %q, want feature-x", got)
	}
}

func TestBranch_DetachedHeadReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWriteFile(t, filepath.Join(dir, ".git", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")

	if got := branch(dir); got != "" {
		t.Errorf("detached should be empty, got %q", got)
	}
}

func TestBranch_NotInRepoReturnsEmpty(t *testing.T) {
	if got := branch(t.TempDir()); got != "" {
		t.Errorf("non-repo should be empty, got %q", got)
	}
}

func TestBranch_EmptyStartReturnsEmpty(t *testing.T) {
	if got := branch(""); got != "" {
		t.Errorf("empty start should be empty, got %q", got)
	}
}

func TestBranch_WorktreeViaGitdirFile(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, "main-repo", ".git")
	mustMkdir(t, realGit)
	worktreeGit := filepath.Join(realGit, "worktrees", "wt1")
	mustMkdir(t, worktreeGit)
	mustWriteFile(t, filepath.Join(worktreeGit, "HEAD"), "ref: refs/heads/wt-branch\n")

	wtDir := filepath.Join(root, "wt1")
	mustMkdir(t, wtDir)
	mustWriteFile(t, filepath.Join(wtDir, ".git"), "gitdir: "+worktreeGit+"\n")

	if got := branch(wtDir); got != "wt-branch" {
		t.Errorf("worktree: got %q, want wt-branch", got)
	}
}

func TestBranch_StopsAtRoot(t *testing.T) {
	if got := branch("/"); got != "" {
		t.Errorf("root should be empty, got %q", got)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWriteFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run TestBranch
```

Expected: build failure (`branch` undefined).

- [ ] **Step 3: Implement git.go**

Create `internal/pkg/render/git.go`:

```go
package render

import (
	"os"
	"path/filepath"
	"strings"
)

const branchRefPrefix = "ref: refs/heads/"

// branch returns the current branch name, walking up from start until it
// finds a .git directory or .git pointer file. Returns "" for: empty start,
// not in a repo, detached HEAD, malformed HEAD, or I/O error.
func branch(start string) string {
	if start == "" {
		return ""
	}
	dir := filepath.Clean(start)
	for i := 0; i < 30; i++ {
		gitDir, ok := resolveGitDir(dir)
		if ok {
			return readHeadBranch(gitDir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// resolveGitDir checks <dir>/.git. If it's a directory, returns it as the
// git dir. If it's a file, parses the "gitdir: <path>" pointer (relative
// paths are resolved against <dir>) and returns the resolved path.
func resolveGitDir(dir string) (string, bool) {
	p := filepath.Join(dir, ".git")
	info, err := os.Lstat(p)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return p, true
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(line, "gitdir: ")
	if !ok {
		return "", false
	}
	if !filepath.IsAbs(rest) {
		rest = filepath.Join(dir, rest)
	}
	return filepath.Clean(rest), true
}

// readHeadBranch parses gitDir/HEAD; returns "" for detached HEAD or any
// read/parse error.
func readHeadBranch(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(s, branchRefPrefix)
	if !ok {
		return ""
	}
	return rest
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/git.go internal/pkg/render/git_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): branch lookup via .git/HEAD without subprocess

Walks up from the supplied start directory (typically
workspace.current_dir from the payload) and reads the branch name
directly from .git/HEAD, supporting worktree gitdir: pointer files.
Detached HEAD and non-repo paths return "". Walk-up is capped at 30
ascents to guard against symlink loops.
EOF
)"
```

---

## Task 3: Render skeleton (types + dispatcher + default rows)

**Files:**
- Create: `internal/pkg/render/render.go`
- Create: `internal/pkg/render/render_test.go`
- Create: `internal/pkg/render/segments.go` (just the registry; segment funcs in later tasks)

- [ ] **Step 1: Write failing tests**

Create `internal/pkg/render/render_test.go`:

```go
package render

import (
	"strings"
	"testing"
)

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
	if !strings.Contains(got, "Opus 4.7") {
		t.Errorf("want model name in output, got %q", got)
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
	got, err := Render(Options{Config: cfg}, []byte(`{}`))
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
	got, _ := Render(Options{Config: cfg}, []byte(`{}`))
	if got != "A | B" {
		t.Errorf("default separator should be ' | ', got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/
```

Expected: build failure (`Render`, `Options`, `Config`, `Segment` undefined). The text segment also doesn't exist yet, but we register it in this task.

- [ ] **Step 3: Implement render.go and segments.go**

Create `internal/pkg/render/render.go`:

```go
package render

import (
	"encoding/json"
	"strings"
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
	Config  Config
	Cwd     string
	NoColor bool
}

// payload mirrors the subset of the Claude Code statusLine payload that
// segments consume. Unknown fields are ignored.
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
	TotalCostUSD       float64 `json:"total_cost_usd"`
	TotalDurationMS    int64   `json:"total_duration_ms"`
	TotalLinesAdded    int64   `json:"total_lines_added"`
	TotalLinesRemoved  int64   `json:"total_lines_removed"`
}
type contextF struct {
	UsedPercentage    float64 `json:"used_percentage"`
	ContextWindowSize int64   `json:"context_window_size"`
	CurrentUsage      struct {
		InputTokens             int64 `json:"input_tokens"`
		OutputTokens            int64 `json:"output_tokens"`
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

	env := renderEnv{cwd: cwd, colorEnabled: !opts.NoColor}

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

// renderEnv carries per-call state to segment functions.
type renderEnv struct {
	cwd          string
	colorEnabled bool
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
```

Create `internal/pkg/render/segments.go`:

```go
package render

// segmentFunc renders one segment. It MUST return "" to suppress the segment
// (the row joiner skips empty results), and MUST NOT return ANSI escape
// codes — colour wrapping happens in renderSegment via style().
type segmentFunc func(p *payload, s Segment, env renderEnv) string

// segmentFuncs is the type registry. Entries are added by init() in this
// file as each segment lands.
var segmentFuncs = map[string]segmentFunc{
	"text": renderText,
}

// renderText returns the segment's Label verbatim. Useful as a literal
// separator or to inject custom strings.
func renderText(_ *payload, s Segment, _ renderEnv) string {
	return s.Label
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/render_test.go internal/pkg/render/segments.go
git commit -s -m "$(cat <<'EOF'
feat(render): Render entry point with row dispatcher and text segment

Adds the public types (Config, Segment, Options), the private payload
struct, the row/segment dispatcher, and the first segment (text). Empty
Config.Rows triggers the built-in default layout; an unknown segment
type renders "?<type>?" so config typos surface without breaking the
row. JSON-parse failures fall back to a one-line model+cwd line so the
status bar is never blank.
EOF
)"
```

---

## Task 4: Text-like segments (model, effort, session_name, output_style, cwd)

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go` (create if absent)

- [ ] **Step 1: Write failing tests**

Create `internal/pkg/render/segments_test.go`:

```go
package render

import "testing"

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
	if got := renderEffort(p, Segment{}, renderEnv{}); got != "effort:xhigh" {
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
	if got := renderEffort(p, Segment{Label: "E"}, renderEnv{}); got != "E:xhigh" {
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
	if got := renderOutputStyle(p, Segment{}, renderEnv{}); got != "style:default" {
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run 'Render(Model|Effort|SessionName|OutputStyle|Cwd)'
```

Expected: build failure (functions undefined).

- [ ] **Step 3: Implement segments**

Append to `internal/pkg/render/segments.go`:

```go
import (
	"path/filepath"
	"strings"
)

func init() {
	segmentFuncs["model"] = renderModel
	segmentFuncs["effort"] = renderEffort
	segmentFuncs["session_name"] = renderSessionName
	segmentFuncs["output_style"] = renderOutputStyle
	segmentFuncs["cwd"] = renderCwd
}

// renderModel returns the model display name. When Show1MFlag is true and
// the payload's exceeds_200k_tokens flag is set, "1M" is appended (any
// existing parenthetical suffix in the display name is dropped first to
// avoid double-flagging like "Opus 4.7 (1M context) 1M").
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

func renderSessionName(p *payload, _ Segment, _ renderEnv) string {
	return p.SessionName
}

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

// renderCwd returns the basename of the workspace.current_dir by default.
// Set Format:"full" on the segment to emit the absolute path.
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
```

(The `import` block goes at the top of segments.go; if the file already has imports, merge.)

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): text-like segments (model, effort, session_name, output_style, cwd)

Five segments rendering string fields directly from the payload. The
model segment strips an existing parenthetical suffix before optionally
appending the 1M flag, so "Opus 4.7 (1M context)" with Show1MFlag and
exceeds_200k_tokens renders as "Opus 4.7 1M" (matching the design's
default layout). cwd defaults to the basename; Format:"full" returns
the absolute path.
EOF
)"
```

---

## Task 5: Numeric segments (cost, duration, lines)

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go`

- [ ] **Step 1: Write failing tests**

Append to `segments_test.go`:

```go
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
	p := &payload{Cost: costF{TotalDurationMS: 7_800_232}}  // ~2h10m
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run 'Render(Cost|Duration|Lines)'
```

Expected: build failure.

- [ ] **Step 3: Implement segments**

Append to `segments.go` and update `init`:

```go
import "fmt"

func init() {
	// existing registrations stay; just add:
	segmentFuncs["cost"]     = renderCost
	segmentFuncs["duration"] = renderDuration
	segmentFuncs["lines"]    = renderLines
}

func renderCost(p *payload, s Segment, _ renderEnv) string {
	format := s.Format
	if format == "" {
		format = "$%.2f"
	}
	return fmt.Sprintf(format, p.Cost.TotalCostUSD)
}

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

func renderLines(p *payload, _ Segment, _ renderEnv) string {
	a, r := p.Cost.TotalLinesAdded, p.Cost.TotalLinesRemoved
	if a == 0 && r == 0 {
		return ""
	}
	return fmt.Sprintf("+%d −%d", a, r) // U+2212 unicode minus
}
```

**Note on duration formatting:** the spec example showed `2h10m` with a leading `0` only for minutes when hours are present (`%02d`). For minutes-only display, no zero pad. For seconds-only, no zero pad. Adjust if you're reading a different example — the test is the contract.

There's one inconsistency to flag in the existing init() blocks: Task 4 added an `init()` to segments.go. This task adds another. Go allows multiple `init()` per file, but for clarity merge them into one. After this task, `segments.go` should have a single `init()` listing all current registrations.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): numeric segments (cost, duration, lines)

cost defaults to $%.2f and accepts a printf-verb override via Format.
duration auto-picks h/m/s scale from total_duration_ms. lines uses the
unicode minus (U+2212) for the removed count and hides itself when both
counts are zero.
EOF
)"
```

---

## Task 6: Context window segment

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go`

- [ ] **Step 1: Write failing tests**

Append to `segments_test.go`:

```go
func TestRenderContext_BarPlusPctDefault(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 27
	p.Context.ContextWindowSize = 1_000_000
	p.Context.CurrentUsage.InputTokens = 273_000
	got := renderContext(p, Segment{Style: "bar+pct"}, renderEnv{})
	// 27% of 16-cell bar = ~4 filled cells
	if got != "[████░░░░░░░░░░░░] 27% 273k/1M" {
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
	if got := renderContext(p, Segment{Style: "bar"}, renderEnv{}); got != "[████░░░░░░░░░░░░]" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_EmptyStyleDefaultsToBarPct(t *testing.T) {
	p := &payload{}
	p.Context.UsedPercentage = 50
	p.Context.ContextWindowSize = 200_000
	p.Context.CurrentUsage.InputTokens = 100_000
	if got := renderContext(p, Segment{}, renderEnv{}); got != "[████████░░░░░░░░] 50% 100k/200k" {
		t.Errorf("got %q", got)
	}
}

func TestRenderContext_HiddenWhenNoData(t *testing.T) {
	if got := renderContext(&payload{}, Segment{}, renderEnv{}); got != "" {
		t.Errorf("expected empty when no data, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run TestRenderContext
```

Expected: build failure.

- [ ] **Step 3: Implement segment**

Add to `segments.go`:

```go
func init() {
	segmentFuncs["context"] = renderContext
}

const barCells = 16

func renderContext(p *payload, s Segment, _ renderEnv) string {
	if p.Context.UsedPercentage == 0 && p.Context.ContextWindowSize == 0 {
		return ""
	}
	style := s.Style
	if style == "" {
		style = "bar+pct"
	}
	pct := int(p.Context.UsedPercentage + 0.5) // round
	bar := makeBar(p.Context.UsedPercentage, barCells)
	switch style {
	case "bar":
		return bar
	case "pct":
		return fmt.Sprintf("%d%%", pct)
	default: // "bar+pct"
		// Use input_tokens for the visible "consumed" number; this matches
		// the spec example "273k/1M" and what ccstatusline shows. Cache
		// tokens (cache_creation/cache_read) are intentionally not summed
		// in — they would inflate the displayed counter beyond what feels
		// intuitive for the user.
		used := p.Context.CurrentUsage.InputTokens
		return fmt.Sprintf("%s %d%% %s/%s",
			bar, pct, formatTokens(used), formatTokens(p.Context.ContextWindowSize))
	}
}

// makeBar renders a unicode block-element bar with `cells` cells filled
// proportionally to pct (0-100).
func makeBar(pct float64, cells int) string {
	filled := int(pct * float64(cells) / 100)
	if filled < 0 {
		filled = 0
	}
	if filled > cells {
		filled = cells
	}
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < filled; i++ {
		b.WriteString("█") // █ FULL BLOCK
	}
	for i := filled; i < cells; i++ {
		b.WriteString("░") // ░ LIGHT SHADE
	}
	b.WriteByte(']')
	return b.String()
}

// formatTokens returns 1234 -> "1k", 273000 -> "273k", 1000000 -> "1M".
// Round-half-down for compactness.
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		// Show 1M, 2M, ... whole millions only if exact; else x.yM
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
```

Note: in the implementation above I deliberately overwrote `used` with just `InputTokens` for the visible counter. That matches the spec's example `273k/1M`. Cache tokens are tracked separately in payload but not shown in the default style.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): context window segment with bar/pct/bar+pct styles

Renders a 16-cell unicode block-element bar, the rounded percentage,
and the consumed/total token counts. The bar+pct style is the default;
bar and pct alone are also supported via Style.
EOF
)"
```

---

## Task 7: Rate limit segments (limit_5h, limit_7d)

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go`

- [ ] **Step 1: Write failing tests**

Append to `segments_test.go`:

```go
func TestRenderLimit5h_PercentAndCountdown(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 7.0
	// resets_at is a unix timestamp; for a deterministic test we install a
	// fake clock via env.now (see implementation).
	p.Limits.FiveHour.ResetsAt = 1_778_412_000 // arbitrary

	env := renderEnv{nowUnix: 1_778_412_000 - (4*3600 + 23*60)}
	got := renderLimit5h(p, Segment{}, env)
	if got != "5h:7% (4h23m)" {
		t.Errorf("got %q, want 5h:7% (4h23m)", got)
	}
}

func TestRenderLimit5h_LabelOverride(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 50
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 100 - 60} // 1m
	if got := renderLimit5h(p, Segment{Label: "WIN"}, env); got != "WIN:50% (1m)" {
		t.Errorf("got %q", got)
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
	if got := renderLimit7d(p, Segment{}, env); got != "7d:30% (5d2h)" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLimit_NegativeRemainingShowsNow(t *testing.T) {
	p := &payload{}
	p.Limits.FiveHour.UsedPercentage = 10
	p.Limits.FiveHour.ResetsAt = 100
	env := renderEnv{nowUnix: 200} // already past
	if got := renderLimit5h(p, Segment{}, env); got != "5h:10% (now)" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run TestRenderLimit
```

Expected: build failure (renderLimit5h, renderLimit7d, renderEnv.nowUnix undefined).

- [ ] **Step 3: Add nowUnix to renderEnv, implement segments**

Modify `render.go` to add `nowUnix` to `renderEnv` and a default in Render:

```go
import "time"

// renderEnv carries per-call state to segment functions.
type renderEnv struct {
	cwd          string
	colorEnabled bool
	nowUnix      int64
}
```

Update `Render`'s `env` construction:

```go
env := renderEnv{
	cwd:          cwd,
	colorEnabled: !opts.NoColor,
	nowUnix:      time.Now().Unix(),
}
```

Add to `segments.go`:

```go
func init() {
	segmentFuncs["limit_5h"] = renderLimit5h
	segmentFuncs["limit_7d"] = renderLimit7d
}

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
	if rl.ResetsAt == 0 {
		return fmt.Sprintf("%s:%s", label, pct)
	}
	return fmt.Sprintf("%s:%s (%s)", label, pct, formatCountdown(rl.ResetsAt-env.nowUnix))
}

// formatPct returns "7%" for 7.0 and "7.5%" for non-integer values.
func formatPct(p float64) string {
	if p == float64(int(p)) {
		return fmt.Sprintf("%d%%", int(p))
	}
	return fmt.Sprintf("%.1f%%", p)
}

// formatCountdown returns a compact relative-time string for a future delta
// in seconds. Negative or zero -> "now".
func formatCountdown(secs int64) string {
	if secs <= 0 {
		return "now"
	}
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd%dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/render.go internal/pkg/render/segments.go internal/pkg/render/segments_test.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
feat(render): rate-limit segments with reset countdown

limit_5h and limit_7d share renderLimit() and a per-segment label
default. Countdowns convert to compact strings (5d2h, 4h23m, 12m, 30s,
or "now" when already past). Tests use a fake nowUnix on renderEnv so
they don't depend on wall clock.
EOF
)"
```

---

## Task 8: Mode segment (thinking + fast_mode flags)

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go`

- [ ] **Step 1: Write failing tests**

```go
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
```

- [ ] **Step 2: Run tests, see them fail**

```bash
go test ./internal/pkg/render/ -run TestRenderMode
```

Expected: build failure.

- [ ] **Step 3: Implement**

```go
func init() {
	segmentFuncs["mode"] = renderMode
}

func renderMode(p *payload, _ Segment, _ renderEnv) string {
	switch {
	case p.Thinking.Enabled:
		return "\U0001F9E0" // 🧠
	case p.FastMode:
		return "⚡" // ⚡
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "feat(render): mode segment for thinking/fast flags"
```

---

## Task 9: git_branch segment

**Files:**
- Modify: `internal/pkg/render/segments.go`
- Modify: `internal/pkg/render/segments_test.go`

- [ ] **Step 1: Write failing tests**

```go
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

	if got := renderGitBranch(&payload{}, Segment{Label: "git"}, renderEnv{cwd: dir}); got != "git:main" {
		t.Errorf("got %q", got)
	}
}
```

(The helpers `mustMkdir`/`mustWriteFile` already exist from Task 2.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/render/ -run TestRenderGitBranch
```

Expected: build failure.

- [ ] **Step 3: Implement**

```go
func init() {
	segmentFuncs["git_branch"] = renderGitBranch
}

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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race ./internal/pkg/render/
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/segments.go internal/pkg/render/segments_test.go
git commit -s -m "feat(render): git_branch segment using .git/HEAD lookup"
```

---

## Task 10: Default-rows golden assertion

**Files:**
- Modify: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Write failing test**

Append to `render_test.go`:

```go
func TestRender_DefaultLayoutAgainstSamplePayload(t *testing.T) {
	raw := []byte(`{
		"model": {"display_name": "Opus 4.7 (1M context)"},
		"workspace": {"current_dir": "/home/u/projects/foo"},
		"exceeds_200k_tokens": true,
		"cost": {"total_cost_usd": 17.7024},
		"context_window": {
			"used_percentage": 27,
			"context_window_size": 1000000,
			"current_usage": {"input_tokens": 273000}
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
	// Three rows joined by \n.
	want := "Opus 4.7 1M | [████░░░░░░░░░░░░] 27% 273k/1M\n" +
		"$17.70 | 5h:7% | 7d:30%\n" +
		"foo"
	// limits print without (countdown) when nowUnix beats resets_at — accept
	// the actual countdown if present. Compare structurally:
	if !strings.HasPrefix(got, "Opus 4.7 1M | [████░░░░░░░░░░░░] 27% 273k/1M\n") {
		t.Errorf("first row mismatch:\n%s\n-- want prefix --\n%s", got, want)
	}
	if !strings.Contains(got, "$17.70 | 5h:7%") {
		t.Errorf("second row missing cost+5h:\n%s", got)
	}
	if !strings.HasSuffix(got, "\nfoo") {
		t.Errorf("last row should be cwd basename:\n%s", got)
	}
}
```

The countdown depends on wall clock vs the stub `resets_at`, so we use prefix/suffix/contains matchers instead of an exact string. The full byte-for-byte assertion is in Task 13 (golden files) where we control both inputs and time.

- [ ] **Step 2: Run test to verify it passes**

The default rows already exercise every segment registered. With the segments from Tasks 4-9 in place, this test should now pass without further code changes.

```bash
go test -race ./internal/pkg/render/ -run TestRender_DefaultLayoutAgainstSamplePayload
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/render/render_test.go
git commit -s -m "test(render): default-rows integration test"
```

---

## Task 11: Wire render.Config into config.Config

**Files:**
- Modify: `internal/pkg/config/config.go`
- Modify: `internal/pkg/config/config_test.go`

- [ ] **Step 1: Write failing test**

Append to `config_test.go`:

```go
import "go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"

func TestSaveLoadRoundtrip_PreservesRenderConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := config.Config{
		Render: render.Config{
			Rows: [][]render.Segment{
				{{Type: "model", Show1MFlag: true}, {Type: "cost"}},
				{{Type: "git_branch"}},
			},
			Separator: " · ",
		},
	}
	if err := config.Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("roundtrip mismatch:\n got %+v\nwant %+v", out, in)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/pkg/config/ -run TestSaveLoadRoundtrip_PreservesRenderConfig
```

Expected: build failure (`config.Config.Render` undefined).

- [ ] **Step 3: Add Render field**

Modify `internal/pkg/config/config.go`:

```go
import "go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"

type Config struct {
	Proxy  Proxy         `json:"proxy,omitzero"`
	Backup Backup        `json:"backup,omitzero"`
	Render render.Config `json:"render,omitzero"`
}
```

- [ ] **Step 4: Run tests to verify all pass**

```bash
go test -race ./...
```

Expected: all pass. Existing config tests are unaffected (Render is omitzero).

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/config/config.go internal/pkg/config/config_test.go
git commit -s -m "$(cat <<'EOF'
feat(config): add Render field for native renderer config

Backwards-compatible: config files without a "render" section continue
to load with Config.Render zero-valued, which the render package
interprets as "use built-in default layout".
EOF
)"
```

---

## Task 12: Integrate render into statusline.Run

**Files:**
- Modify: `internal/pkg/statusline/statusline.go`
- Modify: `internal/pkg/statusline/statusline_test.go`

- [ ] **Step 1: Write failing tests**

Add to `statusline_test.go`:

```go
import "go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"

func TestRun_NativeRenderWhenNoProxy(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/x"}}`
	cfg := render.Config{
		Rows: [][]render.Segment{{{Type: "model"}, {Type: "cwd"}}},
	}
	var out, errOut bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{Render: cfg}, strings.NewReader(body), &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "Opus | x\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRun_NoColorIsRespected(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"}}`
	cfg := render.Config{
		Rows: [][]render.Segment{{{Type: "model", FG: "131"}}},
	}
	var out bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{Render: cfg, NoColor: true},
		strings.NewReader(body), &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("NoColor should suppress ANSI, got %q", out.String())
	}
}

func TestRun_DefaultRenderUsedWhenNoConfigAndNoProxy(t *testing.T) {
	ctx := context.Background()
	body := `{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/tmp"}}`
	var out bytes.Buffer
	err := statusline.Run(ctx, statusline.Options{}, strings.NewReader(body), &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// default layout is multi-line and includes the model name
	if !strings.Contains(out.String(), "Opus") {
		t.Errorf("default layout should include model name, got %q", out.String())
	}
	if !strings.Contains(out.String(), "\n") {
		t.Errorf("default layout should be multi-line, got %q", out.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/statusline/ -run 'TestRun_(NativeRender|NoColor|DefaultRender)'
```

Expected: build failure (`Render`, `NoColor` not in `statusline.Options`).

- [ ] **Step 3: Update statusline.Options and Run**

Modify `internal/pkg/statusline/statusline.go`:

```go
import "go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"

type Options struct {
	ProxyCommand string
	ProxyArgs    []string
	CaptureDir   string
	Render       render.Config
	NoColor      bool
}
```

Replace the `else` branch in `Run` (the old `fmt.Fprintln(outW, render(p))` call) with:

```go
if opts.ProxyCommand != "" {
	runErr = proxy.Run(ctx, opts.ProxyCommand, opts.ProxyArgs, raw, outW, errW)
} else {
	rendered, rerr := render.Render(render.Options{
		Config:  opts.Render,
		Cwd:     p.Workspace.CurrentDir,
		NoColor: opts.NoColor,
	}, raw)
	if rerr != nil {
		runErr = fmt.Errorf("render: %w", rerr)
	} else {
		if _, werr := fmt.Fprintln(outW, rendered); werr != nil {
			runErr = fmt.Errorf("write statusLine: %w", werr)
		}
	}
}
```

Delete the now-unused `render` function and `placeholder` constant from `statusline.go` (they're replaced by `render.Render` + the package's defaultRows).

The existing test `TestRun_FallbackRendersModelAndCurrentDir` and friends now exercise the native renderer's default rows via the same code path. Their expected output strings change from `"Opus · /tmp/proj"` to whatever the default layout emits. **Update these existing tests** to use `strings.Contains` for "Opus" / "/tmp/proj" rather than exact match, since the default layout is multi-line and includes more content.

For the existing `TestRun_FallbackInvalidJSONStillRendersPlaceholder` test: it now exercises `render.Render`'s last-resort fallback. The contract still holds (output non-empty for invalid JSON) but the exact string differs.

- [ ] **Step 4: Run all tests to verify they pass**

```bash
go test -race ./...
```

Expected: all pass after updating the existing fallback-test expectations.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/statusline/statusline.go internal/pkg/statusline/statusline_test.go
git commit -s -m "$(cat <<'EOF'
feat(statusline): delegate non-proxy path to render package

Replaces the trivial "<model> · <dir>" fallback with a call to
render.Render. statusline.Options gains Render (the config) and NoColor
(env-derived) fields. The output is teed through the existing capture
chain, so .out files cover the native-render path the same way they
covered the proxy path.

Existing fallback tests are relaxed from exact-match to substring match
because the default layout is multi-line.
EOF
)"
```

---

## Task 13: cli.Flags struct and main.go wiring

**Files:**
- Modify: `internal/pkg/cli/cli.go`
- Modify: `internal/pkg/cli/cli_test.go`
- Modify: `cmd/ccsb/main.go`

- [ ] **Step 1: Write failing tests**

Append to `cli_test.go`:

```go
func TestRun_PassesNoColorThroughToStatusline(t *testing.T) {
	e := newEnv(t)
	e.saveConfig(t, config.Config{
		Render: render.Config{
			Rows: [][]render.Segment{{{Type: "model", FG: "131"}}},
		},
	})
	body := `{"model":{"display_name":"Opus"}}`
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), e.paths, cli.Flags{NoColor: true}, []string{},
		strings.NewReader(body), &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("NoColor should suppress ANSI, got %q", out.String())
	}
}
```

(Also add a `render` import in cli_test.go.)

The existing tests now need a `cli.Flags{}` argument inserted. There are 14+ call sites to update. This is mechanical.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pkg/cli/
```

Expected: build failure (signature mismatch — `Run` has new parameter).

- [ ] **Step 3: Update cli.Run signature**

Modify `internal/pkg/cli/cli.go`:

```go
type Flags struct {
	NoColor bool
}

func Run(ctx context.Context, p Paths, f Flags, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// (body unchanged for the dispatch logic)
	if len(args) == 0 {
		return runProxy(ctx, p, f, stdin, stdout, stderr)
	}
	// ... same switch ...
}

func runProxy(ctx context.Context, p Paths, f Flags, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}
	return statusline.Run(ctx, statusline.Options{
		ProxyCommand: cfg.Proxy.Command,
		ProxyArgs:    cfg.Proxy.Args,
		CaptureDir:   p.Capture,
		Render:       cfg.Render,
		NoColor:      f.NoColor,
	}, stdin, stdout, stderr)
}
```

- [ ] **Step 4: Update every call site in cli_test.go**

Insert `cli.Flags{}` between `e.paths` and the `args` argument in every `cli.Run(...)` call. Use a global find-and-replace mindful of context.

- [ ] **Step 5: Update main.go**

Modify `cmd/ccsb/main.go`:

```go
import "go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"

func run() error {
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self: %w", err)
	}
	paths := cli.ResolvePaths(cli.Env{
		Home:          os.Getenv("HOME"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		XDGStateHome:  os.Getenv("XDG_STATE_HOME"),
		Self:          self,
	})
	flags := cli.Flags{
		NoColor: os.Getenv("NO_COLOR") != "",
	}
	return cli.Run(ctx, paths, flags, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
```

- [ ] **Step 6: Build, test, smoke**

```bash
go vet ./...
gofmt -l .
go test -race ./...
go build -o bin/ccsb ./cmd/ccsb
echo '{"model":{"display_name":"Opus"},"workspace":{"current_dir":"/tmp"}}' | ./bin/ccsb
```

Expected: vet/fmt clean, tests pass, smoke output is the multi-line default layout.

- [ ] **Step 7: Commit**

```bash
git add internal/pkg/cli/ cmd/ccsb/
git commit -s -m "$(cat <<'EOF'
feat(cli): Flags struct carrying env-derived booleans (NoColor)

cli.Run grows a Flags parameter alongside Paths so env-derived state
travels explicitly without bloating Paths' filesystem-locations
semantic. main.go resolves NO_COLOR once and threads it through.
EOF
)"
```

---

## Task 14: Anonymised payload fixtures + golden tests

**Files:**
- Create: `internal/pkg/render/testdata/payloads/{low_cost,high_cost_1m,near_5h_limit,after_5h_reset,detached_head}.json`
- Create: `internal/pkg/render/testdata/golden/{low_cost,high_cost_1m,near_5h_limit,after_5h_reset,detached_head}.txt`
- Modify: `internal/pkg/render/render_test.go`

- [ ] **Step 1: Curate five fixtures from `~/.local/state/ccsb/captures`**

Pick one capture per scenario and anonymise:
- replace `transcript_path` with `/transcript`
- replace any user-home paths in `cwd` and `workspace.*` with `/repo/path`
- round token counts to nearest 1000
- redact `session_id` to `aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee`

For the **detached_head** case, edit a regular capture to remove the workspace.current_dir or set it to a path that intentionally has no `.git` — the segment renders empty branch in either case, but the test verifies the renderer doesn't crash.

Example anonymisation script (run once, not committed):

```bash
mkdir -p internal/pkg/render/testdata/payloads
# Pick one good capture per scenario by inspecting cost.total_cost_usd and
# rate_limits.five_hour.used_percentage; copy & redact with jq.
```

You must commit the resulting fixture JSONs.

- [ ] **Step 2: Write the golden test**

Append to `render_test.go`:

```go
import (
	"flag"
	"os"
	"path/filepath"
	"time"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestRender_GoldenFixtures(t *testing.T) {
	cases := []string{"low_cost", "high_cost_1m", "near_5h_limit", "after_5h_reset", "detached_head"}
	// Pin nowUnix so countdown formatting is deterministic.
	const fixedNow = int64(1_778_412_000)
	prev := nowFunc
	nowFunc = func() time.Time { return time.Unix(fixedNow, 0) }
	defer func() { nowFunc = prev }()

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			payloadPath := filepath.Join("testdata", "payloads", name+".json")
			goldenPath := filepath.Join("testdata", "golden", name+".txt")
			raw, err := os.ReadFile(payloadPath)
			if err != nil {
				t.Fatalf("read payload: %v", err)
			}
			got, err := Render(Options{}, raw)
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
				t.Errorf("%s mismatch:\n got: %q\nwant: %q", name, got, want)
			}
		})
	}
}
```

This requires introducing `nowFunc` for clock injection. Modify `render.go` to add:

```go
var nowFunc = time.Now

// ... in Render():
env := renderEnv{
	cwd:          cwd,
	colorEnabled: !opts.NoColor,
	nowUnix:      nowFunc().Unix(),
}
```

- [ ] **Step 3: Generate the golden files**

```bash
go test -race ./internal/pkg/render/ -run TestRender_GoldenFixtures -update
```

Inspect the resulting `testdata/golden/*.txt`. Verify each looks correct (multi-line, model name visible, cost present, etc.). Adjust fixtures and re-run if any look wrong.

- [ ] **Step 4: Re-run without -update to confirm parity**

```bash
go test -race ./internal/pkg/render/ -run TestRender_GoldenFixtures
```

Expected: all subtests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/render/testdata/ internal/pkg/render/render.go internal/pkg/render/render_test.go
git commit -s -m "$(cat <<'EOF'
test(render): golden tests over five anonymised real captures

Captures from this session's $XDG_STATE_HOME/ccsb/captures/ pruned to
five representative scenarios (low cost, high cost + 1M, near 5h limit,
right after 5h reset, detached HEAD) with paths and tokens anonymised.
Golden output captured under testdata/golden/ and updateable via
go test ... -update. Time injection via nowFunc so countdowns are
deterministic.
EOF
)"
```

---

## Task 15: End-to-end smoke and final verification

**Files:** none modified

- [ ] **Step 1: Build and run against a synthetic payload**

```bash
go build -o bin/ccsb ./cmd/ccsb
echo '{"model":{"display_name":"Opus 4.7 (1M context)"},"exceeds_200k_tokens":true,"workspace":{"current_dir":"/tmp"},"cost":{"total_cost_usd":17.7024},"context_window":{"used_percentage":27,"context_window_size":1000000,"current_usage":{"input_tokens":273000}},"rate_limits":{"five_hour":{"used_percentage":7.0,"resets_at":2000000000},"seven_day":{"used_percentage":30,"resets_at":2010000000}}}' | ./bin/ccsb
```

Expected: three lines roughly matching the spec's default-layout example, with the 1M flag and rate-limit countdowns shown.

- [ ] **Step 2: Drive native-render path against the user's real config**

Temporarily move the proxy command aside to exercise the native path:

```bash
TMPHOME=$(mktemp -d) TMPSTATE=$(mktemp -d) TMPCFG=$(mktemp -d)
mkdir -p "$TMPHOME/.claude" "$TMPCFG/ccsb"
echo '{"theme":"dark"}' > "$TMPHOME/.claude/settings.json"
echo '{"render":{"rows":[[{"type":"model","show_1m_flag":true},{"type":"cost"}],[{"type":"git_branch"}]]}}' \
  > "$TMPCFG/ccsb/config.json"

echo '{"model":{"display_name":"Opus"},"exceeds_200k_tokens":true,"cost":{"total_cost_usd":1.23},"workspace":{"current_dir":"'"$PWD"'"}}' \
  | HOME=$TMPHOME XDG_CONFIG_HOME=$TMPCFG XDG_STATE_HOME=$TMPSTATE ./bin/ccsb
```

Expected output (two lines):
```
Opus 1M | $1.23
<branch>
```

- [ ] **Step 3: Confirm full project gates**

```bash
go vet ./...
gofmt -l .                          # must be empty
go test -race -cover ./...
```

Expected: vet clean, fmt clean, all tests pass.

- [ ] **Step 4: Final commit (only if anything was tweaked above)**

If steps 1-3 surfaced an issue and you fixed it:

```bash
git add -A
git commit -s -m "fix(render): <what was off>"
```

If nothing needed fixing, no commit.

- [ ] **Step 5: Push the branch**

```bash
git push origin development-0.1.1-work
```

Then follow the project's `git-workflow` (squash to `development-0.1.1-main`, no-ff merge to `main`, optional `production-0.1.2`) when the user asks for it. Do **not** auto-merge.

---

## Self-Review Notes

- **Spec coverage**: every section of the design doc maps to one or more tasks above. Goals (configurable rows, full payload usage, no subprocess) — Tasks 1-9, 11-13. Segment list (14 types) — Tasks 4-9 (model+effort+session_name+output_style+cwd+text already in 3+4; cost+duration+lines in 5; context in 6; limit_5h+limit_7d in 7; mode in 8; git_branch in 9). Color/NO_COLOR — Tasks 1, 12, 13. Git state — Task 2. Default layout — Task 3 + 10. Integration — Tasks 11, 12, 13. Error handling per-segment — Task 3 (unknown type marker). Global parse failure — Task 3 (lastResort). Testing — every task + Task 14.
- **Type consistency**: `Render(opts Options, raw []byte) (string, error)` referenced consistently. `renderEnv{cwd, colorEnabled, nowUnix}` defined in Task 3 and extended in Task 7 (`nowUnix`); the inconsistency between Task 3's struct and Task 7's added field is explicit ("Modify render.go to add nowUnix"). Segment funcs share signature `func(p *payload, s Segment, env renderEnv) string` everywhere.
- **No placeholders**: every step has the actual code or the actual command. Task 14 is the only one with a manual curation step (anonymising captures), and that one explicitly enumerates the redactions.
- **Time injection**: introduced cleanly in Task 7 (`nowUnix` on env) and refined in Task 14 (package-level `nowFunc` for the golden test). Both are explicit in the corresponding tasks.

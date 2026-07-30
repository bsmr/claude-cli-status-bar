package cli_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/cli"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/render"
)

// The wizard asset is instructions to another AI about how to edit a config
// file that ccsb refuses to start without. Three defects have shipped there
// already — phantom segment types (0.4.1), phantom config keys plus a
// whole-file overwrite (0.4.6), and a phantom value SHAPE (0.4.13) — and each
// time the existing tests passed, because they only ever pinned the segment
// type *names*. These two tests close the two remaining gaps: every field the
// code accepts must be mentioned, and every complete example must actually
// parse.

// jsonFieldTags walks t recursively and returns every JSON tag name reachable
// from it, including through nested structs and slice/pointer element types.
func jsonFieldTags(t reflect.Type, seen map[reflect.Type]bool, out map[string]string) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = t.Name() + "." + f.Name
		}
		jsonFieldTags(f.Type, seen, out)
	}
}

// documentedExceptions lists config fields the wizard deliberately does not
// offer. Each needs a reason: this map is the only way to keep a field out of
// the asset, so an unjustified entry is the failure mode to watch for.
var documentedExceptions = map[string]string{
	// ccsb owns these; the wizard is told to preserve the blocks verbatim and
	// must never author their contents.
	"command":              "proxy block — managed by `ccsb mode`, not hand-edited",
	"args":                 "proxy block — managed by `ccsb mode`, not hand-edited",
	"timeout":              "proxy block — bounds the proxy child; a reliability knob, not a layout choice",
	"previous_status_line": "backup block — written by `ccsb install`, restoring it is uninstall's job",
	// Width is the escape hatch for when detection is wrong; margin is the
	// knob users actually reach for, and that one IS documented.
	"width": "diagnostic override for terminal-size detection, not a layout choice",
}

// mentionsConfigKey reports whether the asset documents key in any of the
// forms it legitimately uses: `key` on its own, qualified as `render.key`, or
// as a "key" inside a JSON example. Matching the bare name alone would report
// `render.palette_stride` as undocumented.
func mentionsConfigKey(asset, key string) bool {
	for _, form := range []string{
		"`" + key + "`",
		"." + key + "`",
		`"` + key + `"`,
	} {
		if strings.Contains(asset, form) {
			return true
		}
	}
	return false
}

func TestWizardAsset_EveryConfigFieldIsDocumented(t *testing.T) {
	asset := string(cli.WizardSkillContent())
	if asset == "" {
		t.Fatal("embedded wizard asset is empty")
	}

	tags := map[string]string{}
	seen := map[reflect.Type]bool{}
	for _, root := range []any{config.Config{}, render.Config{}, render.Row{}, render.Segment{}, render.Threshold{}} {
		jsonFieldTags(reflect.TypeOf(root), seen, tags)
	}
	if len(tags) < 20 {
		t.Fatalf("reflection found only %d json tags — the walk is broken, not the asset", len(tags))
	}

	for tag, where := range tags {
		if reason, skipped := documentedExceptions[tag]; skipped {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("exception for %q has no reason", tag)
			}
			continue
		}
		if mentionsConfigKey(asset, tag) {
			continue
		}
		t.Errorf("config field %q (%s) is accepted by the code but never mentioned in the wizard asset —\n"+
			"an AI driving this skill can never produce that configuration, and a rewrite silently drops it.\n"+
			"Document it, or add it to documentedExceptions with a reason.", tag, where)
	}
}

// fencedJSONBlocks returns every ```json fenced block in the asset. The
// convention this test enforces: a ```json block in the wizard is a COMPLETE,
// valid config.json. Partial snippets must use inline code or a different
// fence language, so that everything fenced as json can be parsed as-is.
func fencedJSONBlocks(asset string) []string {
	var blocks []string
	rest := asset
	for {
		i := strings.Index(rest, "```json\n")
		if i < 0 {
			return blocks
		}
		rest = rest[i+len("```json\n"):]
		j := strings.Index(rest, "```")
		if j < 0 {
			return blocks
		}
		blocks = append(blocks, rest[:j])
		rest = rest[j+3:]
	}
}

func TestWizardAsset_EveryJSONExampleParsesAsConfig(t *testing.T) {
	asset := string(cli.WizardSkillContent())
	blocks := fencedJSONBlocks(asset)
	if len(blocks) == 0 {
		t.Fatal("no ```json example in the wizard asset — the canonical example is what pins " +
			"every documented value SHAPE; without one, a wrong shape ships unnoticed again")
	}

	for i, b := range blocks {
		var c config.Config
		if err := json.Unmarshal([]byte(b), &c); err != nil {
			t.Errorf("```json block #%d does not parse as config.json: %v\n---\n%s\n---", i+1, err, b)
			continue
		}
		// Re-marshalling and re-parsing catches a block that unmarshals only
		// because every key was silently ignored.
		if len(c.Render.Rows) == 0 {
			t.Errorf("```json block #%d parsed but produced no rows — a config example that "+
				"configures nothing would let a typo'd key pass:\n%s", i+1, b)
		}
	}
}

// The canonical example must exercise the shapes that have actually gone
// wrong, so that a future edit reintroducing them turns this test red.
func TestWizardAsset_CanonicalExamplePinsTheRiskyShapes(t *testing.T) {
	blocks := fencedJSONBlocks(string(cli.WizardSkillContent()))
	if len(blocks) == 0 {
		t.Fatal("no ```json example in the wizard asset")
	}

	var found struct{ thresholds, powerline, update bool }
	for _, b := range blocks {
		var c config.Config
		if err := json.Unmarshal([]byte(b), &c); err != nil {
			continue
		}
		if c.Update.Auto != "" {
			found.update = true
		}
		if c.Render.Powerline {
			found.powerline = true
		}
		for _, row := range c.Render.Rows {
			for _, s := range row.Segments {
				if len(s.Thresholds) > 0 && s.Thresholds[0].FG != "" {
					found.thresholds = true
				}
			}
		}
	}

	if !found.thresholds {
		t.Error(`no example shows "thresholds" in its real array form ([{"min":70,"fg":"136"}]); ` +
			"the object form documented until 0.4.13 was a hard parse error that blanked the bar")
	}
	if !found.powerline {
		t.Error(`no example sets "powerline": true; it is a plain bool that defaults to false ` +
			"whenever rows are present, so an example omitting it teaches a silent visual downgrade")
	}
	if !found.update {
		t.Error(`no example carries the "update" block; the wizard used to claim config.json had ` +
			"three top-level keys, so a rewrite following it deleted the user's auto-update setting")
	}
}

// The wizard writes `rows`, and any config with `rows` loses the default
// layout's `check_update: true` — which is the only thing that ever consults
// `update.auto`. So a wizard rewrite can silently switch off a user's
// auto-updating without touching the `update` block it was told to preserve.
// v0.4.17 made `ccsb doctor` report the resulting configuration; these two
// tests keep the wizard from producing it in the first place.
//
// Structural, not prose: an example that carries `update.auto` must also carry
// a version segment that enables the check, because an AI copying the example
// copies its shape, whatever the surrounding text says.
func TestWizardAsset_ExamplesWithAutoUpdateAlsoEnableTheCheck(t *testing.T) {
	for _, b := range fencedJSONBlocks(string(cli.WizardSkillContent())) {
		var c config.Config
		if err := json.Unmarshal([]byte(b), &c); err != nil {
			continue // TestWizardAsset_EveryJSONExampleParsesAsConfig owns this
		}
		if c.Update.Auto == "" || len(c.Render.Rows) == 0 {
			continue
		}
		checks := false
		for _, row := range c.Render.Rows {
			for _, s := range row.Segments {
				if s.Type == "version" && s.CheckUpdate {
					checks = true
				}
			}
		}
		if !checks {
			t.Errorf(`an example sets "update.auto": %q alongside its own "rows", but no version `+
				`segment in it sets "check_update": true — copying that layout turns the user's `+
				"auto-updating off while leaving it configured", c.Update.Auto)
		}
	}
}

// And the asset must say why, not merely demonstrate it: an AI editing a
// user's existing rows has no example to copy from. The test asks only that
// some paragraph names both sides of the dependency, so it pins the fact and
// not the wording.
func TestWizardAsset_ExplainsTheAutoUpdateDependency(t *testing.T) {
	for _, para := range strings.Split(string(cli.WizardSkillContent()), "\n\n") {
		if strings.Contains(para, "check_update") && strings.Contains(para, "update.auto") {
			return
		}
	}
	t.Error(`no paragraph of the wizard asset mentions "check_update" and "update.auto" together; ` +
		"nothing tells the model that writing rows without the former disables the latter")
}

// Three claims the asset got wrong until 0.4.25, each verified against the
// renderer rather than against prose. They are pinned the same way as the
// update.auto dependency above — some paragraph must name both sides — so
// the guard survives rewording but not a silent regression to the old,
// wrong statement.
//
// The failure mode being prevented is specific: an AI writing a config from
// this asset believed a typo'd segment type disappears (it prints ?type?),
// that a row may carry align:"right" and still be Powerline (it cannot),
// and that the update marker is always ↑ (it is ⚡ or ⊘ too).
// proseParagraphs splits the asset into paragraphs with fenced code blocks
// removed. A guard that means to scan PROSE must not be satisfiable by an
// example: the align/Powerline case below passed while the paragraph it
// guards was deleted, because the canonical config example happens to
// contain both "align" and "powerline". Found by deleting the paragraph and
// watching the test stay green.
func proseParagraphs(asset string) []string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(asset, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.Split(b.String(), "\n\n")
}

func TestWizardAsset_PinsBehaviourItPreviouslyMisstated(t *testing.T) {
	paras := proseParagraphs(string(cli.WizardSkillContent()))
	cases := []struct {
		name  string
		needs []string
		why   string
	}{
		{
			name:  "an unrecognised segment type is visible in the bar",
			needs: []string{"`type`", "?"},
			why:   "render.go renders an unknown type as ?<type>? and a missing one as ??, not as nothing",
		},
		{
			name:  "row-level align right costs that row its Powerline",
			needs: []string{"align", "owerline"},
			why:   `render.go checks row.Align == "right" before the Powerline branch, so such a row renders plain`,
		},
		{
			name:  "the update marker is not always the up arrow",
			needs: []string{"⚡", "⊘"},
			why:   "updateSeverityStyle uses ⚡ two or more majors ahead, and ⊘ whenever the update is blocked",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, para := range paras {
				all := true
				for _, n := range c.needs {
					if !strings.Contains(para, n) {
						all = false
						break
					}
				}
				if all {
					return
				}
			}
			t.Errorf("no paragraph of the wizard asset mentions %v together — %s", c.needs, c.why)
		})
	}
}

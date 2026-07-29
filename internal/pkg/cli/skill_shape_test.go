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

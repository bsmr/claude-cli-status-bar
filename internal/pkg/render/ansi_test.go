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

func TestStyle_NoAttrsReturnsRawText(t *testing.T) {
	if got := style("hi", "", "", false, true); got != "hi" {
		t.Errorf("with no attrs expected raw text, got %q", got)
	}
}

func TestStyle_WrapsWhenAnySingleAttributeIsSet(t *testing.T) {
	if got := style("hi", "131", "", false, true); got != "\x1b[38;5;131mhi\x1b[0m" {
		t.Errorf("fg only: got %q", got)
	}
	if got := style("hi", "", "220", false, true); got != "\x1b[48;5;220mhi\x1b[0m" {
		t.Errorf("bg only: got %q", got)
	}
	if got := style("hi", "", "", true, true); got != "\x1b[1mhi\x1b[0m" {
		t.Errorf("bold only: got %q", got)
	}
}

// A branch name and a directory name are filesystem content, not ccsb's own
// text, and both reach stdout. A crafted `.git/HEAD` was live-reproduced
// emitting a raw OSC-8 hyperlink plus a newline that added a whole row to the
// bar; a directory whose name carries a BEL does the same through `cwd`.
func TestSanitizePrintable(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary text is untouched", "feature-x", "feature-x"},
		{"printable unicode survives", "läuft-ünicode—✓ 🙂", "läuft-ünicode—✓ 🙂"},
		{"newline goes, or it adds a row to the bar", "a\nb", "ab"},
		{"carriage return goes, or it overwrites the row", "a\rb", "ab"},
		{"tab goes", "a\tb", "ab"},
		{"NUL goes", "a\x00b", "ab"},
		{"DEL goes", "a\x7fb", "ab"},
		{"an OSC-8 hyperlink is defanged", "ma\x1b]8;;http://evil\x07in", "ma]8;;http://evilin"},
		{"an SGR colour escape cannot be smuggled in", "x\x1b[31mred", "x[31mred"},
		{"the space we do want is kept", "on main", "on main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizePrintable(tt.in); got != tt.want {
				t.Errorf("sanitizePrintable(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Invalid UTF-8 must not be dropped silently: it becomes the replacement
// character, which is printable and shows the user something was there.
func TestSanitizePrintable_InvalidUTF8BecomesVisible(t *testing.T) {
	got := sanitizePrintable("a\xffb")
	if got != "a�b" {
		t.Errorf("sanitizePrintable(%q) = %q, want %q", "a\xffb", got, "a�b")
	}
}

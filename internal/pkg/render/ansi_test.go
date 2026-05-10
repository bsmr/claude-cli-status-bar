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

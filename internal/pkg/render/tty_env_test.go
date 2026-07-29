package render

import "testing"

func TestParseWinsizeEnv(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"a plain width", "128", 128},
		{"unset or empty", "", 0},
		{"not a number", "wide", 0},
		{"trailing junk", "128x", 0},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"at the cap", "65535", 65535},
		{"one past the cap", "65536", 0},
		{"absurd", "999999999999", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CCSB_TEST_WINSIZE", c.value)
			if got := parseWinsizeEnv("CCSB_TEST_WINSIZE"); got != c.want {
				t.Errorf("parseWinsizeEnv(%q) = %d, want %d", c.value, got, c.want)
			}
		})
	}
}

func TestEnvWinsizeReader_BothSet(t *testing.T) {
	t.Setenv("COLUMNS", "128")
	t.Setenv("LINES", "48")
	cols, rows, ok := envWinsizeReader()
	if !ok || cols != 128 || rows != 48 {
		t.Errorf("got (%d, %d, %t), want (128, 48, true)", cols, rows, ok)
	}
}

func TestEnvWinsizeReader_ColumnsOnly(t *testing.T) {
	// rows 0 with ok true is the documented shape: the Config.Width branch
	// of discoverTermSize reports exactly the same thing.
	t.Setenv("COLUMNS", "128")
	t.Setenv("LINES", "")
	cols, rows, ok := envWinsizeReader()
	if !ok || cols != 128 || rows != 0 {
		t.Errorf("got (%d, %d, %t), want (128, 0, true)", cols, rows, ok)
	}
}

func TestEnvWinsizeReader_UnusableLinesStillYieldsColumns(t *testing.T) {
	t.Setenv("COLUMNS", "128")
	t.Setenv("LINES", "tall")
	cols, rows, ok := envWinsizeReader()
	if !ok || cols != 128 || rows != 0 {
		t.Errorf("got (%d, %d, %t), want (128, 0, true)", cols, rows, ok)
	}
}

func TestEnvWinsizeReader_NoColumnsMeansNotOK(t *testing.T) {
	// LINES alone is useless: every consumer keys off cols.
	t.Setenv("COLUMNS", "")
	t.Setenv("LINES", "48")
	cols, rows, ok := envWinsizeReader()
	if ok || cols != 0 || rows != 0 {
		t.Errorf("got (%d, %d, %t), want (0, 0, false)", cols, rows, ok)
	}
}

func TestEnvWinsizeReader_UnusableColumnsMeansNotOK(t *testing.T) {
	t.Setenv("COLUMNS", "0")
	t.Setenv("LINES", "48")
	if _, _, ok := envWinsizeReader(); ok {
		t.Error("COLUMNS=0 must not count as a usable width")
	}
}

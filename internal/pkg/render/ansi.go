// ANSI 256-color helpers for the render package. The package doc lives in
// render.go.
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

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
// If none of bold, fg, or bg produces an opening escape, s is also returned
// verbatim so segments without styling do not emit a stray trailing reset.
func style(s, fg, bg string, bold, colorEnabled bool) string {
	if !colorEnabled {
		return s
	}
	fgEsc := fg256(fg)
	bgEsc := bg256(bg)
	if !bold && fgEsc == "" && bgEsc == "" {
		return s
	}
	var b strings.Builder
	if bold {
		b.WriteString("\x1b[1m")
	}
	b.WriteString(fgEsc)
	b.WriteString(bgEsc)
	b.WriteString(s)
	b.WriteString(reset)
	return b.String()
}

// wrapPart colours a sub-region of a segment (its bar glyphs or its
// label) in innerFG and closes back to ambientFG so the surrounding text
// keeps its colour. It mirrors wrapPct's restore trick: close with
// fg256(ambientFG) when that is a valid colour, else the terminal-default
// "\x1b[39m". Returns s verbatim when colour is disabled, innerFG is
// empty or invalid, or innerFG equals ambientFG (no visible change).
func wrapPart(s, innerFG, ambientFG string, colorEnabled bool) string {
	if !colorEnabled || innerFG == "" || innerFG == ambientFG {
		return s
	}
	open := fg256(innerFG)
	if open == "" {
		return s
	}
	closeSeq := "\x1b[39m"
	if reopen := fg256(ambientFG); reopen != "" {
		closeSeq = reopen
	}
	return open + s + closeSeq
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

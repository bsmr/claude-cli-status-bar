// ANSI 256-color helpers for the render package. The package doc lives in
// render.go.
package render

import (
	"strings"
	"unicode"
)

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

// wrapPart colours a sub-region of a segment (its percentage, its bar
// glyphs, or its label) in innerFG and closes back to ambientFG so the
// surrounding text keeps its colour: close with fg256(ambientFG) when
// that is a valid colour, else the terminal-default "\x1b[39m". Returns
// s verbatim when colour is disabled, innerFG is empty or invalid, or
// innerFG equals ambientFG (no visible change). wrapPct is the
// pct-specific adapter over this helper.
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

// sanitizePrintable drops every non-printable rune from s, leaving text that
// cannot drive the terminal.
//
// It is the inbound counterpart to fg256's strict validation: that one keeps a
// malicious *config* from smuggling escapes into ccsb's own output, this one
// keeps content ccsb *reads* from doing the same. Two such sources reach the
// bar — the branch name in `.git/HEAD` and the working directory's name — and
// both are filesystem content, which arrives with a repository rather than
// being chosen by the user. Live-reproduced before this existed: a HEAD
// carrying an OSC-8 hyperlink printed a working link, and its embedded newline
// added an entire row to the status bar.
//
// Dropping rather than escaping is deliberate: the remaining text is what the
// attacker wrote minus its control characters, so `\x1b]8;;url\x07` shows up as
// the visible nonsense `]8;;url` instead of quietly vanishing. Invalid UTF-8
// decodes to U+FFFD, which is printable and therefore kept, for the same
// reason. Only ASCII space survives among the whitespace — a tab or newline in
// a branch name has no legitimate use here, and both damage the layout.
func sanitizePrintable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}

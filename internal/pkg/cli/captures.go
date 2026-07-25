package cli

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/capture"
)

// runCaptures dispatches the `ccsb captures <verb>` subcommand. Only verb so
// far is "clean"; like runConfig it returns a hard error rather than printing
// help, so a typo in a script does not silently succeed.
func runCaptures(p Paths, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("ccsb: captures requires a subcommand (clean)")
	}
	switch args[0] {
	case "clean":
		return runCapturesClean(p, args[1:], stdout)
	default:
		return fmt.Errorf("ccsb: unknown captures subcommand %q (valid: clean)", args[0])
	}
}

// runCapturesClean removes captured payloads and rendered output from the
// capture directory. Without arguments it removes all of them; with
// --older-than it keeps anything newer than the given age.
//
// "Remove everything" is not a separate code path: the cutoff is simply
// time.Now(), and every capture was written before now.
func runCapturesClean(p Paths, args []string, stdout io.Writer) error {
	if p.Capture == "" {
		return fmt.Errorf("ccsb: capture directory path is empty")
	}

	cutoff := time.Now()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--older-than":
			if i+1 >= len(args) {
				return fmt.Errorf("ccsb: --older-than requires a duration (e.g. 7d, 24h)")
			}
			age, err := parseRetention(args[i+1])
			if err != nil {
				return err
			}
			cutoff = time.Now().Add(-age)
			i++
		default:
			return fmt.Errorf("ccsb: unexpected argument %q to captures clean (only --older-than <duration>)", args[i])
		}
	}

	removed, err := capture.Prune(p.Capture, cutoff)
	if err != nil {
		// Prune aborts on the first failure but still reports what it got
		// through, so say so before returning — otherwise a partial sweep
		// looks like it accomplished nothing.
		fmt.Fprintf(stdout, "ccsb: removed %d capture file(s) from %s before stopping\n", removed, p.Capture)
		return err
	}
	if removed == 0 {
		fmt.Fprintf(stdout, "ccsb: no captures to remove in %s\n", p.Capture)
		return nil
	}
	fmt.Fprintf(stdout, "ccsb: removed %d capture file(s) from %s\n", removed, p.Capture)
	return nil
}

// maxRetentionDays is the largest day count that survives conversion to a
// time.Duration. Duration is int64 nanoseconds, so it tops out near 292
// years; beyond that the multiply below wraps — sometimes negative (which
// would push the cutoff into the future and delete everything) and sometimes
// back to a tiny positive value. Both are silent and destructive, so the
// bound is enforced rather than the sign of the product checked.
const maxRetentionDays = math.MaxInt64 / int64(24*time.Hour)

// parseRetention parses a retention age. time.ParseDuration covers h/m/s but
// has no day unit, and days are the natural scale for capture retention, so a
// trailing "d" is converted to hours first. Negative ages are rejected: they
// would push the cutoff into the future and delete everything, which is what
// omitting the flag already does explicitly.
func parseRetention(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("ccsb: cannot parse duration %q (expected e.g. 7d, 24h)", s)
		}
		if n < 0 {
			return 0, fmt.Errorf("ccsb: duration %q is negative", s)
		}
		if int64(n) > maxRetentionDays {
			return 0, fmt.Errorf("ccsb: duration %q is out of range (at most %dd)", s, maxRetentionDays)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("ccsb: cannot parse duration %q (expected e.g. 7d, 24h)", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("ccsb: duration %q is negative", s)
	}
	return d, nil
}

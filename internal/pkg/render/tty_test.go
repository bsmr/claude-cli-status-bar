package render

import "testing"

func TestReadTTYColsFunc_DefaultReturnsZeroOrPositive(t *testing.T) {
	// The real readTTYCols opens /dev/tty. In a `go test` run there may
	// be no controlling tty, in which case 0 is returned. We assert
	// only that the function does not panic and returns a non-negative
	// value.
	got := readTTYCols()
	if got < 0 {
		t.Errorf("readTTYCols(): got %d, want >= 0", got)
	}
}

func TestTTYColsFunc_IsIndirectedForTests(t *testing.T) {
	// ttyColsFunc is a package-level var that tests can swap. Swapping
	// must take effect for the next call.
	prev := ttyColsFunc
	defer func() { ttyColsFunc = prev }()

	ttyColsFunc = func() int { return 128 }
	if got := ttyColsFunc(); got != 128 {
		t.Errorf("ttyColsFunc() with fake: got %d, want 128", got)
	}
}

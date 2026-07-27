package selfupdate

import (
	"path/filepath"
	"testing"
)

func TestGoBinDirPrefersGOBIN(t *testing.T) {
	t.Setenv("GOBIN", "/custom/bin")
	t.Setenv("GOPATH", "/home/u/go")
	if got := goBinDir(); got != "/custom/bin" {
		t.Errorf("goBinDir = %q, want /custom/bin", got)
	}
}

func TestGoBinDirFallsBackToGOPATH(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "/home/u/go")
	if got := goBinDir(); got != filepath.Join("/home/u/go", "bin") {
		t.Errorf("goBinDir = %q, want /home/u/go/bin", got)
	}
}

func TestUseGoInstallMatchesGOBIN(t *testing.T) {
	t.Setenv("GOBIN", t.TempDir())
	if !useGoInstall(filepath.Join(goBinDir(), "ccsb")) {
		t.Error("useGoInstall = false for a binary inside GOBIN, want true")
	}
}

func TestUseGoInstallRejectsOtherLocations(t *testing.T) {
	t.Setenv("GOBIN", t.TempDir())
	if useGoInstall("/opt/ccsb/bin/ccsb") {
		t.Error("useGoInstall = true outside GOBIN, want false")
	}
}

package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	body := []byte(
		"aaaa  ccsb_0.4.8_darwin_arm64.tar.gz\n" +
			"bbbb  ccsb_0.4.8_linux_amd64.tar.gz\n")
	got, ok := parseChecksums(body, "ccsb_0.4.8_linux_amd64.tar.gz")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got != "bbbb" {
		t.Errorf("sum = %q, want %q", got, "bbbb")
	}
}

func TestParseChecksumsMissingFile(t *testing.T) {
	body := []byte("aaaa  ccsb_0.4.8_darwin_arm64.tar.gz\n")
	if _, ok := parseChecksums(body, "ccsb_0.4.8_linux_amd64.tar.gz"); ok {
		t.Error("ok = true for an absent file, want false")
	}
}

func TestDownloadVerified(t *testing.T) {
	payload := []byte("release-archive-bytes")
	sum := sha256.Sum256(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	got, err := downloadVerified(context.Background(), srv.URL, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("downloadVerified: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("body = %q, want %q", got, payload)
	}
}

func TestDownloadVerifiedHashMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer srv.Close()

	_, err := downloadVerified(context.Background(), srv.URL, strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("err = nil, want a checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("err = %v, want it to mention the checksum", err)
	}
}

func TestDownloadVerifiedNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such release", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := downloadVerified(context.Background(), srv.URL, strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("err = nil, want a status error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want it to mention the status code", err)
	}
}

// releaseServer serves a release whose archive is body, and points
// releaseBaseURL at it for the duration of the test.
func releaseServer(t *testing.T, version string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), assetName(version))
		case strings.HasSuffix(r.URL.Path, assetName(version)):
			_, _ = w.Write(body)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	restore := releaseBaseURL
	releaseBaseURL = srv.URL
	t.Cleanup(func() { releaseBaseURL = restore })
}

func TestFetchAsset(t *testing.T) {
	want := []byte("archive-bytes")
	releaseServer(t, "0.4.8", want)

	got, err := fetchAsset(context.Background(), "0.4.8")
	if err != nil {
		t.Fatalf("fetchAsset: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("archive = %q, want %q", got, want)
	}
}

func TestFetchAssetUnlistedPlatform(t *testing.T) {
	// The checksums file exists but names no archive for this platform.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("aaaa  ccsb_0.4.8_plan9_sparc.tar.gz\n"))
	}))
	defer srv.Close()
	restore := releaseBaseURL
	releaseBaseURL = srv.URL
	defer func() { releaseBaseURL = restore }()

	_, err := fetchAsset(context.Background(), "0.4.8")
	if err == nil {
		t.Fatal("err = nil, want a missing-asset error")
	}
	if !strings.Contains(err.Error(), assetName("0.4.8")) {
		t.Errorf("err = %v, want it to name the missing asset", err)
	}
}

func TestAssetURL(t *testing.T) {
	got := assetURL("https://example.test/dl", "0.4.8")
	want := fmt.Sprintf("https://example.test/dl/v0.4.8/%s", assetName("0.4.8"))
	if got != want {
		t.Errorf("assetURL = %q, want %q", got, want)
	}
}

// tarGz builds a gzipped tar containing one entry per name/content pair.
func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	archive := tarGz(t, map[string]string{
		"LICENSE": "MIT",
		"ccsb":    "#!/bin/sh\necho hi\n",
	})
	dir := t.TempDir()

	path, err := extractBinary(archive, dir)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("extracted to %q, want a file inside %q", path, dir)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\necho hi\n" {
		t.Errorf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("mode = %v, want the owner-execute bit set", info.Mode().Perm())
	}
}

func TestExtractBinaryMissingEntry(t *testing.T) {
	archive := tarGz(t, map[string]string{"README.md": "docs"})
	if _, err := extractBinary(archive, t.TempDir()); err == nil {
		t.Fatal("err = nil, want a missing-entry error")
	}
}

func TestExtractBinaryCorruptArchive(t *testing.T) {
	if _, err := extractBinary([]byte("not a gzip stream"), t.TempDir()); err == nil {
		t.Fatal("err = nil, want a gzip error")
	}
}

func TestExtractBinaryIgnoresNestedPaths(t *testing.T) {
	// An entry named with a traversal prefix must not escape destDir: the
	// destination name is chosen by extractBinary, never taken from the tar.
	archive := tarGz(t, map[string]string{"../../evil/ccsb": "payload"})
	path, err := extractBinary(archive, t.TempDir())
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if strings.Contains(path, "evil") {
		t.Errorf("path = %q, want a name chosen by extractBinary", path)
	}
}

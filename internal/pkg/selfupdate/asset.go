package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// maxAssetBytes bounds the release archive download. The real archive is
	// roughly 6 MB; this leaves generous headroom while refusing to buffer an
	// unbounded response from a third party.
	maxAssetBytes = 64 << 20

	// assetFetchTimeout bounds the path-B network work, checksums plus
	// archive. The update check's 5s covers a small JSON body and is far too
	// tight for a multi-megabyte download.
	assetFetchTimeout = 60 * time.Second
)

// releaseBaseURL is the download root for release assets. A package variable
// (not a constant) purely so tests can redirect it to an httptest.Server;
// production code never reassigns it.
var releaseBaseURL = "https://github.com/bsmr/claude-cli-status-bar/releases/download"

// assetName builds the archive file name GoReleaser produces for this
// platform. Note the asymmetry with the tag: the file name carries no
// leading "v", the tag does.
func assetName(version string) string {
	return fmt.Sprintf("ccsb_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

// assetURL builds the download URL for this platform's archive.
func assetURL(base, version string) string {
	return fmt.Sprintf("%s/v%s/%s", base, version, assetName(version))
}

// checksumsURL builds the download URL for the release's checksums file.
func checksumsURL(base, version string) string {
	return fmt.Sprintf("%s/v%s/checksums.txt", base, version)
}

// parseChecksums finds file's SHA-256 in a GoReleaser checksums.txt, whose
// lines are "<sum>  <name>". ok is false when the file is not listed.
func parseChecksums(body []byte, file string) (string, bool) {
	for _, line := range strings.Split(string(body), "\n") {
		sum, name, found := strings.Cut(strings.TrimSpace(line), "  ")
		if found && name == file {
			return sum, true
		}
	}
	return "", false
}

// fetch retrieves url with a bound on how much of the body is read.
func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ccsb")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// downloadVerified fetches url and returns its bytes only when they hash to
// wantSHA. A mismatch is fatal — it means the download was corrupted in
// transit. It does not defend against a compromised release: the checksum
// file ships from the same place as the asset.
func downloadVerified(ctx context.Context, url, wantSHA string) ([]byte, error) {
	body, err := fetch(ctx, url, maxAssetBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, wantSHA) {
		return nil, fmt.Errorf("checksum mismatch: got %s, want %s", got, wantSHA)
	}
	return body, nil
}

// fetchAsset downloads the release archive for target and returns its bytes,
// verified against the release's checksums file.
//
// The download deadline lives here rather than around the whole path-B update
// so that a slow but legitimate multi-megabyte download cannot eat into the
// smoke test's budget and destroy an otherwise good staged binary.
func fetchAsset(ctx context.Context, target string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, assetFetchTimeout)
	defer cancel()

	sums, err := fetch(ctx, checksumsURL(releaseBaseURL, target), maxAssetBytes)
	if err != nil {
		return nil, err
	}
	want, ok := parseChecksums(sums, assetName(target))
	if !ok {
		return nil, fmt.Errorf("release v%s has no asset for %s", target, assetName(target))
	}
	return downloadVerified(ctx, assetURL(releaseBaseURL, target), want)
}

// maxExtractBytes bounds how many bytes are written while extracting, so a
// crafted archive cannot fill the disk. Twice the download bound: a
// compressed archive legitimately expands.
const maxExtractBytes = 128 << 20

// extractBinary writes the archive's "ccsb" entry to a fresh temp file in
// destDir and returns its path. destDir must be the directory the binary
// will ultimately be renamed into, so that rename stays on one filesystem
// and therefore atomic.
//
// Entry names from the archive are matched against, never joined into a
// path: the destination name comes from os.CreateTemp. Path traversal is
// thus excluded by construction rather than by sanitising input.
func extractBinary(archive []byte, destDir string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("archive contains no ccsb binary")
		}
		if err != nil {
			return "", fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "ccsb" {
			continue
		}
		return writeTempBinary(tr, destDir)
	}
}

// writeTempBinary copies r into a new executable temp file in destDir.
func writeTempBinary(r io.Reader, destDir string) (string, error) {
	f, err := os.CreateTemp(destDir, ".ccsb-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	path := f.Name()
	written, err := io.Copy(f, io.LimitReader(r, maxExtractBytes))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written == maxExtractBytes {
		err = errors.New("archive entry exceeds the extraction bound")
	}
	if err == nil {
		err = os.Chmod(path, 0o700)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

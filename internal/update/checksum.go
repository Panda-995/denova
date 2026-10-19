package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// releaseChecksum binds verification to the release selected before downloading.
func (s *Service) releaseChecksum(ctx context.Context, release githubRelease, assetName string) (string, error) {
	asset := selectChecksumAsset(release.Assets)
	if asset == nil {
		return "", fmt.Errorf("release is missing checksums.txt; refusing to install %q", assetName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAssetDownloadURL(*asset), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksums: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName {
			digest, err := hex.DecodeString(fields[0])
			if err != nil || len(digest) != sha256.Size {
				return "", fmt.Errorf("invalid SHA-256 for %s", assetName)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt is missing %s", assetName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

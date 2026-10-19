package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Downloads live under their SHA-256 identity. Partial files survive transient
// network failures; grab verifies the complete content, including resumed bytes.
func (s *Service) downloadAsset(ctx context.Context, url, target string, expectedSize int64, digest string, progress func(InstallProgress)) error {
	if actual, err := fileSHA256(target); err == nil && actual == digest {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, updateDownloadTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	checksum, err := hex.DecodeString(digest)
	if err != nil || len(checksum) != sha256.Size {
		return fmt.Errorf("invalid update checksum")
	}
	client := grab.NewClient()
	client.HTTPClient = s.downloadHTTPClient()
	var downloadErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := grab.NewRequest(target+".download", url)
		if err != nil {
			return err
		}
		req = req.WithContext(ctx)
		req.Size = expectedSize
		req.SetChecksum(sha256.New(), checksum, true)
		req.HTTPRequest.Header.Set("Accept", "application/octet-stream")
		req.HTTPRequest.Header.Set("User-Agent", "denova-updater")
		slog.InfoContext(ctx, "update_download_started", "asset", filepath.Base(target), "attempt", attempt)
		resp := client.Do(req)
		ticker := time.NewTicker(200 * time.Millisecond)
	downloading:
		for {
			select {
			case <-ticker.C:
				reportInstallProgress(progress, downloadProgress(filepath.Base(target), target, resp, expectedSize))
			case <-resp.Done:
				break downloading
			}
		}
		ticker.Stop()
		downloadErr = resp.Err()
		if downloadErr == nil {
			if err := renameUpdatePath(target+".download", target); err != nil {
				return err
			}
			reportInstallProgress(progress, downloadProgress(filepath.Base(target), target, resp, expectedSize))
			return nil
		}
		if errors.Is(downloadErr, grab.ErrBadLength) {
			_ = os.Remove(target + ".download")
		}
		var networkError net.Error
		var status grab.StatusCodeError
		retryable := errors.Is(downloadErr, io.ErrUnexpectedEOF) || errors.Is(downloadErr, grab.ErrBadChecksum) || errors.Is(downloadErr, grab.ErrBadLength) || errors.As(downloadErr, &networkError) || (errors.As(downloadErr, &status) && (status == 429 || status >= 500))
		if !retryable || attempt == 3 || ctx.Err() != nil {
			break
		}
		slog.WarnContext(ctx, "update_download_retry", "attempt", attempt, "error", downloadErr)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
		}
	}
	return fmt.Errorf("download update package: %w", downloadErr)
}

func downloadProgress(assetName, archivePath string, resp *grab.Response, expectedSize int64) InstallProgress {
	total := maxInt64(resp.Size(), expectedSize)
	downloaded := resp.BytesComplete()
	percent := resp.Progress() * 100
	if total > 0 && downloaded > 0 {
		percent = float64(downloaded) / float64(total) * 100
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return InstallProgress{
		Phase:           "downloading",
		AssetName:       assetName,
		ArchivePath:     archivePath,
		DownloadedBytes: downloaded,
		TotalBytes:      total,
		Percent:         percent,
	}
}

package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/cesargomez89/navidrums/internal/catalog"
	"github.com/cesargomez89/navidrums/internal/config"
	"github.com/cesargomez89/navidrums/internal/constants"
	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/ffmpeg"
	"github.com/cesargomez89/navidrums/internal/storage"
)

// ProgressFunc reports transfer progress. total is 0 when the server did not
// declare a size, in which case only downloaded is meaningful.
type ProgressFunc func(downloaded, total int64)

type Downloader interface {
	Download(ctx context.Context, track *domain.Track, destPathNoExt string, quality string, logger *slog.Logger, onProgress ProgressFunc) (string, error)
}

type downloader struct {
	providerManager *catalog.ProviderManager
	config          *config.Config
}

func NewDownloader(pm *catalog.ProviderManager, cfg *config.Config) Downloader {
	return &downloader{
		providerManager: pm,
		config:          cfg,
	}
}

func (d *downloader) Download(ctx context.Context, track *domain.Track, destPathNoExt string, quality string, logger *slog.Logger, onProgress ProgressFunc) (string, error) {
	provider := d.providerManager.GetDownloadProvider()

	// Lossless payloads can arrive as FLAC inside an MP4 container: that is the
	// only manifest type Monochrome instances serve. Remuxing to .flac keeps the
	// library consistent and is lossless; tagging an .m4a needs ffmpeg anyway.
	shouldConvertToFLAC := quality == constants.QualityHiResLossless || quality == constants.QualityLossless

	var lastErr error

	for attempt := 0; attempt < constants.DefaultRetryCount; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		stream, mimeType, err := provider.GetStream(ctx, track.ProviderID, track.ISRC, quality)
		if err != nil {
			lastErr = err
			logger.Error("Download attempt failed",
				"attempt", attempt+1,
				"total_attempts", constants.DefaultRetryCount,
				"track_id", track.ID,
				"track_title", track.Title,
				"provider_id", track.ProviderID,
				"error", err,
			)
			time.Sleep(time.Duration(attempt+1) * constants.DefaultRetryBase)
			continue
		}

		ext := constants.ExtFLAC
		switch mimeType {
		case constants.MimeTypeMP4:
			ext = constants.ExtM4A
		case constants.MimeTypeMP3:
			ext = constants.ExtMP3
		}

		downloadPath := destPathNoExt + ext

		f, err := storage.CreateFile(downloadPath)
		if err != nil {
			_ = stream.Close()
			continue
		}

		var total int64
		if sizer, ok := stream.(catalog.StreamSizer); ok {
			total = sizer.Size()
		}

		_, err = io.Copy(f, newProgressReader(stream, total, onProgress))
		_ = stream.Close()
		_ = f.Close()

		if err != nil {
			lastErr = err
			_ = storage.RemoveFile(downloadPath)
			time.Sleep(time.Duration(attempt+1) * constants.DefaultRetryBase)
			continue
		}

		if shouldConvertToFLAC && mimeType == constants.MimeTypeMP4 {
			flacPath, convErr := ffmpeg.ConvertToFLAC(ctx, downloadPath)
			if convErr != nil {
				lastErr = convErr
				_ = storage.RemoveFile(downloadPath)
				time.Sleep(time.Duration(attempt+1) * constants.DefaultRetryBase)
				continue
			}

			if err := storage.RemoveFile(downloadPath); err != nil {
				// We won't retry just because cleanup failed, success is the .flac
				return flacPath, nil
			}

			return flacPath, nil
		}

		return downloadPath, nil
	}

	return "", fmt.Errorf("download failed after %d attempts: %w", constants.DefaultRetryCount, lastErr)
}

// progressReadInterval throttles progress reporting: a lossless track is a few
// thousand reads and each report writes to SQLite.
const progressReadInterval = 750 * time.Millisecond

// progressReader reports how much of a stream has been read.
type progressReader struct {
	reader     io.Reader
	onProgress ProgressFunc
	total      int64
	downloaded int64
	lastReport time.Time
}

func newProgressReader(reader io.Reader, total int64, onProgress ProgressFunc) io.Reader {
	if onProgress == nil {
		return reader
	}
	return &progressReader{reader: reader, total: total, onProgress: onProgress}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.downloaded += int64(n)

	// Report on a timer, and always on the final read so the bar completes.
	if time.Since(r.lastReport) >= progressReadInterval || err == io.EOF {
		r.lastReport = time.Now()
		r.onProgress(r.downloaded, r.total)
	}

	return n, err
}

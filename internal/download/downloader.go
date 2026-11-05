package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/zhimoe/bingwallpaper/internal/fsutil"
	"github.com/zhimoe/bingwallpaper/internal/httpx"
)

// Downloader streams remote assets into local files.
type Downloader struct{ HTTP *http.Client }

// SaveOptions controls optional-asset handling and in-memory retention.
type SaveOptions struct {
	Optional      bool
	KeepContent   bool
	ValidateImage bool
}

// Result reports the saved size and optionally retained content.
type Result struct {
	Content []byte
	Size    int64
}

// Save downloads rawURL and atomically replaces destination.
func (d Downloader) Save(ctx context.Context, rawURL, destination string, options SaveOptions) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if options.Optional {
			slog.Warn("可选资源下载失败，已跳过", "url", rawURL, "error", err)
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("下载 %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if options.Optional {
			slog.Debug("可选资源不可用，已跳过", "url", rawURL, "status", resp.Status)
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("下载 %s: %s", rawURL, resp.Status)
	}
	const maxAsset = int64(256 << 20)
	if resp.ContentLength > maxAsset {
		if options.Optional {
			slog.Warn("可选资源超过大小限制，已跳过", "url", rawURL, "size", resp.ContentLength)
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("资源超过 256 MiB: %s", rawURL)
	}

	// Read a small prefix before creating the destination so error pages and
	// other non-image responses never replace a good cached wallpaper.
	var head []byte
	if options.ValidateImage {
		buf := make([]byte, 16)
		n, readErr := io.ReadFull(resp.Body, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			if options.Optional {
				slog.Warn("可选资源读取失败，已跳过", "url", rawURL, "error", readErr)
				return Result{}, nil
			}
			return Result{}, fmt.Errorf("读取 %s: %w", rawURL, readErr)
		}
		head = buf[:n]
		if !isImagePrefix(head) {
			if options.Optional {
				slog.Warn("可选资源不是图片，已跳过", "url", rawURL, "content_type", resp.Header.Get("Content-Type"))
				return Result{}, nil
			}
			return Result{}, fmt.Errorf("下载 %s: 内容不是图片 (Content-Type %q)", rawURL, resp.Header.Get("Content-Type"))
		}
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return Result{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".bingwallpaper-*")
	if err != nil {
		return Result{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	var content bytes.Buffer
	var writer io.Writer = tmp
	if options.KeepContent {
		writer = io.MultiWriter(tmp, &content)
	}
	body := io.Reader(resp.Body)
	if len(head) > 0 {
		body = io.MultiReader(bytes.NewReader(head), body)
	}
	size, err := io.Copy(writer, io.LimitReader(body, maxAsset+1))
	emptyOptional := false
	if err == nil && size > maxAsset {
		err = fmt.Errorf("资源超过 256 MiB: %s", rawURL)
	}
	if err == nil && size == 0 {
		if options.Optional {
			emptyOptional = true
		} else {
			err = fmt.Errorf("下载 %s: 返回空资源", rawURL)
		}
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		if options.Optional {
			slog.Warn("可选资源保存失败，已跳过", "url", rawURL, "error", err)
			return Result{}, nil
		}
		return Result{}, err
	}
	if emptyOptional {
		return Result{}, nil
	}
	if err := fsutil.ReplaceFile(tmpName, destination); err != nil {
		return Result{}, err
	}
	return Result{Content: content.Bytes(), Size: size}, nil
}

// isImagePrefix recognizes the image formats Bing and custom servers can
// return. BMP only uses two bytes, so that check is intentionally permissive.
func isImagePrefix(prefix []byte) bool {
	switch {
	case len(prefix) >= 3 && prefix[0] == 0xff && prefix[1] == 0xd8 && prefix[2] == 0xff:
		return true // JPEG
	case len(prefix) >= 8 && bytes.Equal(prefix[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return true // PNG
	case len(prefix) >= 6 && (string(prefix[:6]) == "GIF87a" || string(prefix[:6]) == "GIF89a"):
		return true // GIF
	case len(prefix) >= 2 && prefix[0] == 'B' && prefix[1] == 'M':
		return true // BMP
	case len(prefix) >= 12 && string(prefix[:4]) == "RIFF" && string(prefix[8:12]) == "WEBP":
		return true // WebP
	default:
		return false
	}
}

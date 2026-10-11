package lib

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexferrari88/sbstck-dl/internal/heicpreview"
)

// saveImage is shared by body and cover downloads. Source bytes are published
// atomically before conversion, so failed previews still retain the download.
func (id *ImageDownloader) saveImage(ctx context.Context, source, path string, body io.Reader) ImageInfo {
	result := ImageInfo{OriginalURL: source, OriginalPath: path, LocalPath: path}
	err := writeFileAtomicallyWith(path, func(file *os.File) error {
		size, err := io.Copy(file, &contextImageReader{ctx: ctx, reader: body})
		if err != nil {
			return err
		}
		if size == 0 {
			return fmt.Errorf("downloaded image is empty")
		}
		return ctx.Err()
	})
	if err != nil {
		result.Error = fmt.Errorf("failed to save image: %w", err)
		return result
	}
	return id.localImage(ctx, source, path)
}

type contextImageReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextImageReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// A content digest distinguishes revisions and preserves complete previews used
// by previously published posts, even if a replacement cannot be converted.
func originalPreviewPath(path string) string {
	digestDir := filepath.Dir(path)
	if filepath.Base(filepath.Dir(digestDir)) != ".sbstck-previews" || len(filepath.Base(digestDir)) != 64 || !strings.HasSuffix(path, ".png") {
		return path
	}
	return filepath.Join(filepath.Dir(filepath.Dir(digestDir)), strings.TrimSuffix(filepath.Base(path), ".png"))
}

func (id *ImageDownloader) localImage(ctx context.Context, source, path string) ImageInfo {
	result := ImageInfo{OriginalURL: source, OriginalPath: path, LocalPath: path}
	if err := ctx.Err(); err != nil {
		result.Error = err
		return result
	}
	file, err := os.Open(path)
	if err != nil {
		result.Error = err
		return result
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, err := reader.Peek(512)
	if err != nil && err != io.EOF {
		result.Error = err
		return result
	}
	if len(header) == 0 {
		result.Error = fmt.Errorf("downloaded image is empty")
		return result
	}
	if !needsHEICPreview(header, source, path) {
		result.Format = id.getImageFormat(filepath.Base(path))
		result.Width, result.Height = id.extractDimensionsFromURL(source)
		result.Success = ctx.Err() == nil
		result.Error = ctx.Err()
		return result
	}
	data, err := io.ReadAll(io.LimitReader(&contextImageReader{ctx: ctx, reader: reader}, heicpreview.MaxInputBytes+1))
	if err != nil {
		result.Error = err
		return result
	}
	if len(data) > heicpreview.MaxInputBytes {
		result.Error = fmt.Errorf("HEIC preview exceeds 64 MiB input limit (original retained)")
		return result
	}
	digest := sha256.Sum256(data)
	result.LocalPath = filepath.Join(filepath.Dir(path), ".sbstck-previews", fmt.Sprintf("%x", digest), filepath.Base(path)+".png")
	// A complete, bounded PNG is proof of conversion of these exact bytes.
	if cfg, err := cachedPreview(result.LocalPath); err == nil {
		result.Width, result.Height, result.Format = cfg.Width, cfg.Height, "png"
		result.Success = ctx.Err() == nil
		result.Error = ctx.Err()
		return result
	}

	img, err := heicpreview.Decode(ctx, data)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		result.Error = fmt.Errorf("failed to convert HEIC preview (original retained): %w", err)
		return result
	}
	if err := os.MkdirAll(filepath.Dir(result.LocalPath), 0755); err != nil {
		result.Error = err
		return result
	}
	err = writeFileAtomicallyWith(result.LocalPath, func(file *os.File) error {
		if err := png.Encode(file, img); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		result.Error = fmt.Errorf("failed to save HEIC preview (original retained): %w", err)
		return result
	}
	result.Width, result.Height = img.Bounds().Dx(), img.Bounds().Dy()
	result.Format, result.Success = "png", true
	return result
}

func cachedPreview(path string) (image.Config, error) {
	var zero image.Config
	file, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return zero, fmt.Errorf("preview is not a regular file")
	}
	cfg, err := png.DecodeConfig(file)
	if err != nil {
		return zero, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > heicpreview.MaxPixels/cfg.Height {
		return zero, fmt.Errorf("preview exceeds pixel limit")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return zero, err
	}
	if _, err := png.Decode(io.LimitReader(file, heicpreview.MaxPixels*5)); err != nil {
		return zero, err
	}
	return cfg, nil
}

func needsHEICPreview(header []byte, source, path string) bool {
	// CDN transforms may serve ordinary PNG/JPEG/WebP bytes at a HEIC URL.
	mime := http.DetectContentType(header)
	if strings.HasPrefix(mime, "image/") {
		return false
	}
	if len(header) >= 16 && bytes.Equal(header[4:8], []byte("ftyp")) {
		boxSize := uint64(binary.BigEndian.Uint32(header[:4]))
		brandOffset := 8
		if boxSize == 1 { // ISO BMFF extended-size box
			if len(header) < 24 {
				return false
			}
			boxSize = binary.BigEndian.Uint64(header[8:16])
			brandOffset = 16
		}
		if boxSize == 0 || boxSize > uint64(len(header)) {
			boxSize = uint64(len(header))
		}
		size := int(boxSize)
		// AVIF uses the same generic HEIF brands but must retain its behavior.
		for offset := brandOffset; offset+4 <= size; offset += 4 {
			if offset == brandOffset+4 {
				continue
			}
			brand := string(header[offset : offset+4])
			if brand == "avif" || brand == "avis" {
				return false
			}
		}
		for offset := brandOffset; offset+4 <= size; offset += 4 {
			if offset == brandOffset+4 {
				continue
			} // minor version, not a compatible brand
			switch string(header[offset : offset+4]) {
			case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
				return true
			}
		}
	}
	for _, candidate := range []string{source, path} {
		if parsed, err := url.Parse(candidate); err == nil {
			candidate = parsed.Path
		}
		for i := 0; i < 2; i++ {
			decoded, err := url.PathUnescape(candidate)
			if err != nil || decoded == candidate {
				break
			}
			candidate = decoded
		}
		ext := strings.ToLower(filepath.Ext(candidate))
		if ext == ".heic" || ext == ".heif" {
			return true
		}
	}
	return false
}

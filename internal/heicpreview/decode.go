// Adapted from github.com/gen2brain/heic v0.4.8 (MIT); see LICENSE and README.md.
package heicpreview

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"io"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed lib/heif.wasm.gz
var heifWasm []byte

var decoderSlot = make(chan struct{}, 1)

func Decode(ctx context.Context, data []byte) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case decoderSlot <- struct{}{}:
		defer func() { <-decoderSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	img, _, err := decode(ctx, data)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return img, err
}

func decode(ctx context.Context, data []byte) (image.Image, image.Config, error) {
	var cfg image.Config

	if len(data) > MaxInputBytes {
		return nil, cfg, fmt.Errorf("HEIC exceeds 64 MiB input limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, cfg, err
	}
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(8192).WithCloseOnContextDone(true))
	defer rt.Close(context.Background())
	r, err := gzip.NewReader(bytes.NewReader(heifWasm))
	if err != nil {
		return nil, cfg, err
	}
	wasm, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return nil, cfg, err
	}
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, cfg, err
	}
	mod, err := rt.InstantiateWithConfig(ctx, wasm, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, cfg, err
	}

	defer mod.Close(ctx)

	_alloc := mod.ExportedFunction("malloc")
	_free := mod.ExportedFunction("free")
	_decode := mod.ExportedFunction("decode")

	inSize := len(data)

	res, err := _alloc.Call(ctx, uint64(inSize))
	if err != nil {
		return nil, cfg, fmt.Errorf("alloc: %w", err)
	}
	inPtr := res[0]
	defer _free.Call(ctx, inPtr)

	ok := mod.Memory().Write(uint32(inPtr), data)
	if !ok {
		return nil, cfg, ErrMemWrite
	}

	res, err = _alloc.Call(ctx, 4*5)
	if err != nil {
		return nil, cfg, fmt.Errorf("alloc: %w", err)
	}
	defer _free.Call(ctx, res[0])

	widthPtr := res[0]
	heightPtr := res[0] + 4
	colorspacePtr := res[0] + 8
	chromaPtr := res[0] + 12
	premultipliedPtr := res[0] + 16

	res, err = _decode.Call(ctx, inPtr, uint64(inSize), 1, widthPtr, heightPtr, colorspacePtr, chromaPtr, premultipliedPtr, 0)
	if err != nil {
		return nil, cfg, fmt.Errorf("decode: %w", err)
	}

	if res[0] == 0 {
		return nil, cfg, ErrDecode
	}

	width, ok := mod.Memory().ReadUint32Le(uint32(widthPtr))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	height, ok := mod.Memory().ReadUint32Le(uint32(heightPtr))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	colorspace, ok := mod.Memory().ReadUint32Le(uint32(colorspacePtr))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	chroma, ok := mod.Memory().ReadUint32Le(uint32(chromaPtr))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	premultiplied, ok := mod.Memory().ReadUint32Le(uint32(premultipliedPtr))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	cfg.Width = int(width)
	cfg.Height = int(height)

	switch colorspace {
	case heifColorspaceYCbCr:
		cfg.ColorModel = color.YCbCrModel
	case heifColorspaceMonochrome:
		cfg.ColorModel = color.GrayModel
	case heifColorspaceRGB:
		if premultiplied == 1 {
			cfg.ColorModel = color.RGBAModel
		} else {
			cfg.ColorModel = color.NRGBAModel
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, cfg, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxPixels/cfg.Height {
		return nil, cfg, fmt.Errorf("HEIC exceeds 50 megapixel limit")
	}

	var size int
	var stride int
	var w, h, cw, ch int
	var i0, i1, i2 int
	var subsampleRatio image.YCbCrSubsampleRatio

	rect := image.Rect(0, 0, cfg.Width, cfg.Height)

	switch colorspace {
	case heifColorspaceYCbCr:
		switch chroma {
		case heifChroma420:
			subsampleRatio = image.YCbCrSubsampleRatio420
		case heifChroma422:
			subsampleRatio = image.YCbCrSubsampleRatio422
		case heifChroma444:
			subsampleRatio = image.YCbCrSubsampleRatio444
		default:
			return nil, cfg, fmt.Errorf("unsupported chroma %d", chroma)
		}

		w, h, cw, ch = yCbCrSize(rect, subsampleRatio)
		w = alignm(w)
		cw = alignm(cw)

		i0 = w * h
		i1 = w*h + 1*cw*ch
		i2 = w*h + 2*cw*ch

		size = i2
	case heifColorspaceMonochrome:
		stride = alignm(cfg.Width * 1)
		size = cfg.Height * stride
	case heifColorspaceRGB:
		stride = alignm(cfg.Width * 4)
		size = cfg.Height * stride
	default:
		return nil, cfg, fmt.Errorf("unsupported colorspace %d", colorspace)
	}

	res, err = _alloc.Call(ctx, uint64(size))
	if err != nil {
		return nil, cfg, fmt.Errorf("alloc: %w", err)
	}
	outPtr := res[0]
	defer _free.Call(ctx, outPtr)

	res, err = _decode.Call(ctx, inPtr, uint64(inSize), 0, widthPtr, heightPtr, colorspacePtr, chromaPtr, premultipliedPtr, outPtr)
	if err != nil {
		return nil, cfg, fmt.Errorf("decode: %w", err)
	}

	if res[0] == 0 {
		return nil, cfg, ErrDecode
	}

	out, ok := mod.Memory().Read(uint32(outPtr), uint32(size))
	if !ok {
		return nil, cfg, ErrMemRead
	}

	out = append([]byte(nil), out...) // Detach pixels before closing WASM memory.
	var img image.Image

	switch colorspace {
	case heifColorspaceYCbCr:
		img = &image.YCbCr{
			Y:              out[:i0:i0],
			Cb:             out[i0:i1:i1],
			Cr:             out[i1:i2:i2],
			SubsampleRatio: subsampleRatio,
			YStride:        w,
			CStride:        cw,
			Rect:           rect,
		}
	case heifColorspaceMonochrome:
		img = &image.Gray{
			Pix:    out,
			Stride: stride,
			Rect:   rect,
		}
	case heifColorspaceRGB:
		if premultiplied == 1 {
			img = &image.RGBA{
				Pix:    out,
				Stride: stride,
				Rect:   rect,
			}
		} else {
			img = &image.NRGBA{
				Pix:    out,
				Stride: stride,
				Rect:   rect,
			}
		}
	}

	return img, cfg, nil
}

var (
	ErrMemRead  = fmt.Errorf("HEIC memory read failed")
	ErrMemWrite = fmt.Errorf("HEIC memory write failed")
	ErrDecode   = fmt.Errorf("HEIC decode failed")
)

const (
	MaxInputBytes = 64 << 20
	MaxPixels     = 50_000_000
	alignSize     = 16

	heifColorspaceYCbCr      = 0
	heifColorspaceRGB        = 1
	heifColorspaceMonochrome = 2

	heifChroma420 = 1
	heifChroma422 = 2
	heifChroma444 = 3
)

func alignm(a int) int {
	return (a + (alignSize - 1)) & (^(alignSize - 1))
}

func yCbCrSize(r image.Rectangle, subsampleRatio image.YCbCrSubsampleRatio) (w, h, cw, ch int) {
	w, h = r.Dx(), r.Dy()

	switch subsampleRatio {
	case image.YCbCrSubsampleRatio422:
		cw = (r.Max.X+1)/2 - r.Min.X/2
		ch = h
	case image.YCbCrSubsampleRatio420:
		cw = (r.Max.X+1)/2 - r.Min.X/2
		ch = (r.Max.Y+1)/2 - r.Min.Y/2
	default:
		cw = w
		ch = h
	}

	return
}

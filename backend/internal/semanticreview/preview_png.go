package semanticreview

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image/color"
	"image/png"
	"io"
)

// VerifyExposurePreviewPNG accepts only the worker's 8-bit RGB/RGBA wire format.
// This proves encoding/profile hygiene, not photographic quality or mask replay.
func VerifyExposurePreviewPNG(ctx context.Context, raw []byte, digest string, width, height int,
	srgbSHA, role string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	overlay := role == "affected" || role == "protected"
	if (!overlay && role != "before" && role != "after") || width < 1 || height < 1 || width > 2560 || height > 2560 ||
		len(raw) < 33 || len(raw) > 33882112 || !shaPattern.MatchString(digest) {
		return ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest || !bytes.Equal(raw[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return ErrInvalid
	}
	offset := 8
	header := false
	profile := false
	data := false
	ended := false
	colorType := byte(0)
	for offset < len(raw) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(raw)-offset < 12 {
			return ErrInvalid
		}
		length := int64(binary.BigEndian.Uint32(raw[offset : offset+4]))
		if length > int64(len(raw)-offset-12) {
			return ErrInvalid
		}
		end := offset + 8 + int(length)
		kind := string(raw[offset+4 : offset+8])
		payload := raw[offset+8 : end]
		if crc32.ChecksumIEEE(raw[offset+4:end]) != binary.BigEndian.Uint32(raw[end:end+4]) {
			return ErrInvalid
		}
		if !header && kind != "IHDR" {
			return ErrInvalid
		}
		switch kind {
		case "IHDR":
			if header || len(payload) != 13 || int64(binary.BigEndian.Uint32(payload[:4])) != int64(width) || int64(binary.BigEndian.Uint32(payload[4:8])) != int64(height) || payload[8] != 8 || payload[10] != 0 || payload[11] != 0 || payload[12] != 0 {
				return ErrInvalid
			}
			colorType = payload[9]
			if colorType != 2 && colorType != 6 || overlay && colorType != 6 {
				return ErrInvalid
			}
			header = true
		case "iCCP":
			if overlay || profile || data || len(payload) > 2*1048576 || !shaPattern.MatchString(srgbSHA) {
				return ErrInvalid
			}
			split := bytes.IndexByte(payload, 0)
			if split < 1 || split > 79 || split+2 >= len(payload) || payload[split+1] != 0 {
				return ErrInvalid
			}
			compressed := bytes.NewReader(payload[split+2:])
			reader, err := zlib.NewReader(compressed)
			if err != nil {
				return ErrInvalid
			}
			icc, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || len(icc) < 128 || len(icc) > 1048576 || compressed.Len() != 0 {
				return ErrInvalid
			}
			pin := sha256.Sum256(icc)
			if hex.EncodeToString(pin[:]) != srgbSHA {
				return ErrInvalid
			}
			profile = true
		case "IDAT":
			if !overlay && !profile {
				return ErrInvalid
			}
			data = true
		case "IEND":
			if len(payload) != 0 || !data || end+4 != len(raw) {
				return ErrInvalid
			}
			ended = true
		default:
			return ErrInvalid
		}
		offset = end + 4
	}
	if !ended || !header || !data || !overlay && !profile {
		return ErrInvalid
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return ErrInvalid
	}
	if overlay {
		for y := 0; y < height; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			for x := 0; x < width; x++ {
				// NRGBA preserves RGB beneath transparent pixels, unlike premultiplied RGBA.
				c := decoded.At(x, y)
				n := color.NRGBAModel.Convert(c).(color.NRGBA)
				if role == "affected" && (n.R != 255 || n.G != 80 || n.B != 40) || role == "protected" && (n.R != 0 || n.G != 220 || n.B != 255) {
					return ErrInvalid
				}
			}
		}
	}
	return ctx.Err()
}

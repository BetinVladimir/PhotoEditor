package masknpy

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"hash/crc32"
	"io"
	"math"
)

// VerifyDefectLabelPNG proves exact uint16 labels in the worker's PNG16 wire
// format (grayscale, noninterlaced, filter0), without allocating a whole image.
// Both original streams are caller-owned; integrity is checked through EOF.
func VerifyDefectLabelPNG(ctx context.Context, labels, png PinnedMask, width, height int64) error {
	return verifyMaskPNG(ctx, labels, png, width, height, false)
}

// VerifyProbabilityMaskPNG checks the worker's exact float64 round-to-even
// quantization of finite float32 [0,1] probabilities into PNG16 samples.
func VerifyProbabilityMaskPNG(ctx context.Context, pixels, png PinnedMask, width, height int64) error {
	return verifyMaskPNG(ctx, pixels, png, width, height, true)
}

func verifyMaskPNG(ctx context.Context, labels, png PinnedMask, width, height int64, probability bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return ErrInvalid
	}
	raw := height * (width*2 + 1)
	digest, err := hex.DecodeString(png.SHA256)
	if png.Reader == nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != png.SHA256 || png.Bytes < 57 || png.Bytes > raw+raw/50+1048576 {
		return ErrInvalid
	}
	dtype, componentBytes := "<u2", int64(2)
	if probability {
		dtype, componentBytes = "<f4", 4
	}
	npy, err := openTypedPayload(ctx, labels, width, height, dtype, componentBytes)
	if err != nil {
		return err
	}
	sum := sha256.New()
	limited := &io.LimitedReader{R: png.Reader, N: png.Bytes + 1}
	p := &labelPNGStream{ctx: ctx, reader: io.TeeReader(limited, sum)}
	var header [33]byte
	if err := readUserMaskBytes(ctx, p.reader, header[:]); err != nil {
		return err
	}
	if !bytes.Equal(header[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) || binary.BigEndian.Uint32(header[8:12]) != 13 || string(header[12:16]) != "IHDR" || binary.BigEndian.Uint32(header[16:20]) != uint32(width) || binary.BigEndian.Uint32(header[20:24]) != uint32(height) || !bytes.Equal(header[24:29], []byte{16, 0, 0, 0, 0}) || crc32.ChecksumIEEE(header[12:29]) != binary.BigEndian.Uint32(header[29:33]) {
		return ErrInvalid
	}
	if err := p.nextChunk(); err != nil {
		return err
	}
	if p.ended {
		return ErrInvalid
	}
	decoded, err := zlib.NewReader(p)
	if err != nil {
		return err
	}
	defer decoded.Close()
	var left [65536]byte
	var right [32768]byte
	for y := int64(0); y < height; y++ {
		var filter [1]byte
		if err := readUserMaskBytes(ctx, decoded, filter[:]); err != nil {
			return err
		}
		if filter[0] != 0 {
			return ErrInvalid
		}
		for remaining := width; remaining > 0; {
			pixels := int64(len(right) / 2)
			if remaining < pixels {
				pixels = remaining
			}
			if err := readUserMaskBytes(ctx, npy.reader, left[:int(pixels*componentBytes)]); err != nil {
				return err
			}
			if err := readUserMaskBytes(ctx, decoded, right[:int(pixels*2)]); err != nil {
				return err
			}
			for i := int64(0); i < pixels; i++ {
				var expected uint16
				if probability {
					value := math.Float32frombits(binary.LittleEndian.Uint32(left[i*4 : i*4+4]))
					if !(value >= 0 && value <= 1) {
						return ErrInvalid
					}
					expected = uint16(math.RoundToEven(float64(value) * 65535))
				} else {
					expected = binary.LittleEndian.Uint16(left[i*2 : i*2+2])
				}
				if expected != binary.BigEndian.Uint16(right[i*2:i*2+2]) {
					return ErrInvalid
				}
			}
			remaining -= pixels
		}
	}
	var tail [1]byte
	n, err := io.ReadFull(decoded, tail[:])
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if n != 0 || err != io.EOF {
		return ErrInvalid
	}
	if err := decoded.Close(); err != nil {
		return err
	}
	if err := p.finish(); err != nil {
		return err
	}
	n, err = io.ReadFull(p.reader, tail[:])
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if n != 0 || err != io.EOF || limited.N != 1 || !bytes.Equal(sum.Sum(nil), digest) {
		return ErrInvalid
	}
	if err := finishMaskPayload(ctx, npy); err != nil {
		return err
	}
	return ctx.Err()
}

// Implements ByteReader so zlib does not read ahead past its own Adler checksum.
// At most one <=64KiB IDAT chunk is consumed at a time; CRC is checked in place.
type labelPNGStream struct {
	ctx       context.Context
	reader    io.Reader
	remaining uint32
	crc       hash.Hash32
	ended     bool
}

func (p *labelPNGStream) nextChunk() error {
	var header [8]byte
	if err := readUserMaskBytes(p.ctx, p.reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:4])
	kind := string(header[4:])
	if kind == "IEND" {
		if size != 0 {
			return ErrInvalid
		}
		var crc [4]byte
		if err := readUserMaskBytes(p.ctx, p.reader, crc[:]); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(crc[:]) != crc32.ChecksumIEEE(header[4:]) {
			return ErrInvalid
		}
		p.ended = true
		return nil
	}
	if kind != "IDAT" || size == 0 || size > 65536 {
		return ErrInvalid
	}
	p.remaining = size
	p.crc = crc32.NewIEEE()
	_, _ = p.crc.Write(header[4:])
	return nil
}
func (p *labelPNGStream) completeChunk() error {
	if p.crc == nil {
		return nil
	}
	var checksum [4]byte
	if err := readUserMaskBytes(p.ctx, p.reader, checksum[:]); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(checksum[:]) != p.crc.Sum32() {
		return ErrInvalid
	}
	p.crc = nil
	return nil
}
func (p *labelPNGStream) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if p.remaining == 0 {
		if err := p.completeChunk(); err != nil {
			return 0, err
		}
		if p.ended {
			return 0, io.EOF
		}
		if err := p.nextChunk(); err != nil {
			return 0, err
		}
		if p.ended {
			return 0, io.EOF
		}
	}
	size := len(b)
	if uint32(size) > p.remaining {
		size = int(p.remaining)
	}
	n, err := p.reader.Read(b[:size])
	if n > 0 {
		_, _ = p.crc.Write(b[:n])
		p.remaining -= uint32(n)
	}
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (p *labelPNGStream) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(p, b[:])
	return b[0], err
}
func (p *labelPNGStream) finish() error {
	if p.remaining != 0 {
		return ErrInvalid
	}
	if err := p.completeChunk(); err != nil {
		return err
	}
	if !p.ended {
		if err := p.nextChunk(); err != nil {
			return err
		}
	}
	if !p.ended {
		return ErrInvalid
	}
	return nil
}

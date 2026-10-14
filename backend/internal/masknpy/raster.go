package masknpy

import (
	"context"
	"encoding/binary"
	"math"
)

// RasterSamples supplies independently reconstructed probabilities for one row
// segment. It must return owned exact-length target/protection slices.
type RasterSamples func(context.Context, int64, int64, int64) ([]float32, []float32, error)

// VerifyRasterProbabilityNPY compares both complete SHA-pinned original streams
// with independent expected samples. No approximate probability tolerance is used.
// Readers remain caller-owned; success requires exact header/payload/SHA/EOF.
func VerifyRasterProbabilityNPY(ctx context.Context, target, protected PinnedMask, width, height int64, expected RasterSamples) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if expected == nil || width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return 0, ErrInvalid
	}
	a, err := openMaskPayload(ctx, target, width, height)
	if err != nil {
		return 0, err
	}
	b, err := openMaskPayload(ctx, protected, width, height)
	if err != nil {
		return 0, err
	}
	var left, right [4096]byte
	var nonzero int64
	for y := int64(0); y < height; y++ {
		for x := int64(0); x < width; x += 1024 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			length := min(int64(1024), width-x)
			wantTarget, wantProtection, err := expected(ctx, x, y, length)
			if err != nil {
				return 0, err
			}
			if int64(len(wantTarget)) != length || int64(len(wantProtection)) != length {
				return 0, ErrInvalid
			}
			size := int(length * 4)
			if err := readUserMaskBytes(ctx, a.reader, left[:size]); err != nil {
				return 0, err
			}
			if err := readUserMaskBytes(ctx, b.reader, right[:size]); err != nil {
				return 0, err
			}
			for i := 0; i < int(length); i++ {
				tb, pb := binary.LittleEndian.Uint32(left[i*4:i*4+4]), binary.LittleEndian.Uint32(right[i*4:i*4+4])
				t, p := math.Float32frombits(tb), math.Float32frombits(pb)
				if !(t >= 0 && t <= 1 && p >= 0 && p <= 1) || tb != math.Float32bits(wantTarget[i]) || pb != math.Float32bits(wantProtection[i]) {
					return 0, ErrInvalid
				}
				if p > 0 {
					nonzero++
				}
			}
		}
	}
	if err := finishMaskPayload(ctx, a); err != nil {
		return 0, err
	}
	if err := finishMaskPayload(ctx, b); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return nonzero, nil
}

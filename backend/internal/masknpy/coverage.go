package masknpy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var ErrCoverageMissing = errors.New("protection does not cover required mask")

// VerifyMaskCoverage requires positive protection at every positive required
// pixel. Protection uses strict boolean support, not probability magnitude.
// Empty required coverage is valid only after complete integrity validation.
// Caller must independently prove source geometry, origin and policy authority;
// this function does not infer people/defects from an arbitrary mask.
// Two 64 KiB buffers bound payload memory. Readers remain caller-owned.
func VerifyMaskCoverage(ctx context.Context, required, protected PinnedMask, width, height int64) (int64, error) {
	return verifyMaskCoverage(ctx, required, protected, width, height, false)
}

// VerifyProbabilityMaskCoverage preserves the required protection magnitude:
// each protected value must be at least the corresponding original required
// probability. Both complete original streams must pass integrity checks.
func VerifyProbabilityMaskCoverage(ctx context.Context, required, protected PinnedMask, width, height int64) (int64, error) {
	return verifyMaskCoverage(ctx, required, protected, width, height, true)
}

func verifyMaskCoverage(ctx context.Context, required, protected PinnedMask, width, height int64, magnitude bool) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return 0, ErrInvalid
	}
	a, err := openMaskPayload(ctx, required, width, height)
	if err != nil {
		return 0, err
	}
	b, err := openMaskPayload(ctx, protected, width, height)
	if err != nil {
		return 0, err
	}
	var left, right [65536]byte
	nonempty := int64(0)
	missing := false
	for remaining := 4 * width * height; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		size := int64(len(left))
		if remaining < size {
			size = remaining
		}
		if err := readUserMaskBytes(ctx, a.reader, left[:int(size)]); err != nil {
			return 0, err
		}
		if err := readUserMaskBytes(ctx, b.reader, right[:int(size)]); err != nil {
			return 0, err
		}
		for i := 0; i < int(size); i += 4 {
			x := math.Float32frombits(binary.LittleEndian.Uint32(left[i : i+4]))
			y := math.Float32frombits(binary.LittleEndian.Uint32(right[i : i+4]))
			if !(x >= 0 && x <= 1 && y >= 0 && y <= 1) {
				return 0, ErrInvalid
			}
			if (x > 0 && y == 0) || (magnitude && y < x) {
				missing = true
			}
			if x > 0 {
				nonempty++
			}
		}
		remaining -= size
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
	// Missing coverage is a content classification only after both complete
	// original byte streams pass numeric, length and SHA validation. A weak early
	// pixel must not hide later corruption or turn it into an unqualified model.
	if missing {
		return 0, fmt.Errorf("%w: %w", ErrInvalid, ErrCoverageMissing)
	}
	return nonempty, nil
}

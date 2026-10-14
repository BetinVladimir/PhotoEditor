package masknpy

import (
	"context"
	"encoding/binary"
	"math"
)

// VerifyFaceSkinNPY proves exact mask.face_skin derivation: uint16 Face19 class1
// becomes float32 1, every other valid class0..18 becomes 0. Returns skin pixel
// count only after both original SHA/size/EOF checks. Readers stay caller-owned;
// two buffers bound memory independently of source dimensions.
func VerifyFaceSkinNPY(ctx context.Context, labels, skin PinnedMask, width, height int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return 0, ErrInvalid
	}
	a, err := openTypedPayload(ctx, labels, width, height, "<u2", 2)
	if err != nil {
		return 0, err
	}
	b, err := openMaskPayload(ctx, skin, width, height)
	if err != nil {
		return 0, err
	}
	var left [32768]byte
	var right [65536]byte
	count := int64(0)
	for remaining := width * height; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		pixels := int64(len(left) / 2)
		if remaining < pixels {
			pixels = remaining
		}
		if err := readUserMaskBytes(ctx, a.reader, left[:int(pixels*2)]); err != nil {
			return 0, err
		}
		if err := readUserMaskBytes(ctx, b.reader, right[:int(pixels*4)]); err != nil {
			return 0, err
		}
		for i := 0; i < int(pixels); i++ {
			label := binary.LittleEndian.Uint16(left[i*2 : i*2+2])
			value := math.Float32frombits(binary.LittleEndian.Uint32(right[i*4 : i*4+4]))
			if label > 18 {
				return 0, ErrInvalid
			}
			expected := float32(0)
			if label == 1 {
				expected = 1
				count++
			}
			if value != expected {
				return 0, ErrInvalid
			}
		}
		remaining -= pixels
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
	return count, nil
}

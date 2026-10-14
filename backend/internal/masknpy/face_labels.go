package masknpy

import (
	"context"
	"encoding/binary"
)

// VerifyFaceLabelsNPY validates an original hash-pinned C-order <u2 Face19 array.
// Counts are returned only after exact size/hash/EOF verification. Memory is
// bounded to one 32KiB buffer; the caller owns the reader. This does not certify
// inference or model qualification.
func VerifyFaceLabelsNPY(ctx context.Context, pin PinnedMask, width, height int64) ([19]int64, error) {
	empty := [19]int64{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return empty, ErrInvalid
	}
	payload, err := openTypedPayload(ctx, pin, width, height, "<u2", 2)
	if err != nil {
		return empty, err
	}
	var counts [19]int64
	var buffer [32768]byte
	for remaining := width * height; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		pixels := int64(len(buffer) / 2)
		if remaining < pixels {
			pixels = remaining
		}
		if err := readUserMaskBytes(ctx, payload.reader, buffer[:int(pixels*2)]); err != nil {
			return empty, err
		}
		for i := 0; i < int(pixels); i++ {
			label := binary.LittleEndian.Uint16(buffer[i*2 : i*2+2])
			if label > 18 {
				return empty, ErrInvalid
			}
			counts[label]++
		}
		remaining -= pixels
	}
	if err := finishMaskPayload(ctx, payload); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return counts, nil
}

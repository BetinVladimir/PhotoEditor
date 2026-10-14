package masknpy

import (
	"context"
	"encoding/binary"
	"math"
)

// LabelBounds must come from a separately verified defect proposal. This checks
// physical labels only; it does not approve candidates or infer protection.
type LabelBounds struct {
	Label               uint16
	X, Y, Width, Height int64
}
type LabelEvidence struct {
	Label  uint16
	Pixels int64
}

// VerifyDefectLabelsNPY requires the exact candidate set and minimal bounding
// rectangles in a hash-pinned C-order little-endian uint16 NPY. Memory is bounded
// by 4096 candidates and a 64KiB stream buffer. Caller owns the reader.
func VerifyDefectLabelsNPY(ctx context.Context, pin PinnedMask, width, height int64, candidates []LabelBounds) ([]LabelEvidence, error) {
	return verifyDefectLabelsNPY(ctx, pin, nil, width, height, candidates)
}

// VerifyDefectLabelsProtectionNPY additionally verifies a complete original
// float32 protection stream and rejects every label on positive protection.
// Empty proposals remain valid. Both SHA/size/EOF checks precede evidence.
func VerifyDefectLabelsProtectionNPY(ctx context.Context, labels, protection PinnedMask, width, height int64, candidates []LabelBounds) ([]LabelEvidence, error) {
	return verifyDefectLabelsNPY(ctx, labels, &protection, width, height, candidates)
}

func verifyDefectLabelsNPY(ctx context.Context, pin PinnedMask, protection *PinnedMask, width, height int64, candidates []LabelBounds) ([]LabelEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width || len(candidates) > 4096 {
		return nil, ErrInvalid
	}
	candidates = append([]LabelBounds(nil), candidates...)
	type aggregate struct {
		bounds                        LabelBounds
		count, xmin, ymin, xmax, ymax int64
	}
	lookup := make(map[uint16]*aggregate, len(candidates))
	for _, c := range candidates {
		if c.Label == 0 || lookup[c.Label] != nil || c.X < 0 || c.Y < 0 || c.Width < 1 || c.Height < 1 || c.X >= width || c.Y >= height || c.Width > width-c.X || c.Height > height-c.Y {
			return nil, ErrInvalid
		}
		lookup[c.Label] = &aggregate{bounds: c, xmin: width, ymin: height, xmax: -1, ymax: -1}
	}
	payload, err := openTypedPayload(ctx, pin, width, height, "<u2", 2)
	if err != nil {
		return nil, err
	}
	var protected maskPayload
	if protection != nil {
		protected, err = openMaskPayload(ctx, *protection, width, height)
		if err != nil {
			return nil, err
		}
	}
	var buffer [32768]byte
	var protectedBuffer [65536]byte
	position := int64(0)
	for remaining := 2 * width * height; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		size := int64(len(buffer))
		if remaining < size {
			size = remaining
		}
		if err := readUserMaskBytes(ctx, payload.reader, buffer[:int(size)]); err != nil {
			return nil, err
		}
		if protection != nil {
			if err := readUserMaskBytes(ctx, protected.reader, protectedBuffer[:int(size*2)]); err != nil {
				return nil, err
			}
		}
		for i := 0; i < int(size); i += 2 {
			label := binary.LittleEndian.Uint16(buffer[i : i+2])
			if protection != nil {
				value := math.Float32frombits(binary.LittleEndian.Uint32(protectedBuffer[i*2 : i*2+4]))
				if !(value >= 0 && value <= 1) || (label != 0 && value > 0) {
					return nil, ErrInvalid
				}
			}
			if label != 0 {
				entry := lookup[label]
				if entry == nil {
					return nil, ErrInvalid
				}
				x, y := position%width, position/width
				entry.count++
				if x < entry.xmin {
					entry.xmin = x
				}
				if x > entry.xmax {
					entry.xmax = x
				}
				if y < entry.ymin {
					entry.ymin = y
				}
				if y > entry.ymax {
					entry.ymax = y
				}
			}
			position++
		}
		remaining -= size
	}
	if err := finishMaskPayload(ctx, payload); err != nil {
		return nil, err
	}
	if protection != nil {
		if err := finishMaskPayload(ctx, protected); err != nil {
			return nil, err
		}
	}
	result := make([]LabelEvidence, 0, len(candidates))
	for _, c := range candidates {
		e := lookup[c.Label]
		if e.count == 0 || e.xmin != c.X || e.ymin != c.Y || e.xmax-e.xmin+1 != c.Width || e.ymax-e.ymin+1 != c.Height {
			return nil, ErrInvalid
		}
		result = append(result, LabelEvidence{Label: c.Label, Pixels: e.count})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

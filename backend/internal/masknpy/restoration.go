package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
)

func openRestorationRGB(ctx context.Context, pin PinnedMask, width, height int64) (maskPayload, error) {
	var zero maskPayload
	digest, err := hex.DecodeString(pin.SHA256)
	if pin.Reader == nil || err != nil || len(digest) != 32 ||
		hex.EncodeToString(digest) != pin.SHA256 {
		return zero, ErrInvalid
	}
	dataBytes := width * height * 3 * 4
	if pin.Bytes < dataBytes+10 || pin.Bytes > dataBytes+65548 {
		return zero, ErrInvalid
	}
	h := sha256.New()
	r := io.TeeReader(pin.Reader, h)
	var prefix [8]byte
	if err = readUserMaskBytes(ctx, r, prefix[:]); err != nil {
		return zero, err
	}
	if !bytes.Equal(prefix[:6], []byte{0x93, 'N', 'U', 'M', 'P', 'Y'}) || prefix[7] != 0 {
		return zero, ErrInvalid
	}
	lengthSize := 2
	switch prefix[6] {
	case 1:
	case 2, 3:
		lengthSize = 4
	default:
		return zero, ErrInvalid
	}
	var length [4]byte
	if err = readUserMaskBytes(ctx, r, length[:lengthSize]); err != nil {
		return zero, err
	}
	headerBytes := int64(binary.LittleEndian.Uint16(length[:2]))
	if lengthSize == 4 {
		headerBytes = int64(binary.LittleEndian.Uint32(length[:]))
	}
	if headerBytes < 1 || headerBytes > 65536 || int64(8+lengthSize)+headerBytes+dataBytes != pin.Bytes {
		return zero, ErrInvalid
	}
	header := make([]byte, int(headerBytes))
	if err = readUserMaskBytes(ctx, r, header); err != nil {
		return zero, err
	}
	if !userMaskHeaderShape(header, []int64{height, width, 3}, "<f4") {
		return zero, ErrInvalid
	}
	return maskPayload{r, h, digest}, nil
}

// VerifyRestorationPixelDelta streams exact source/output RGB and mask NPY
// versions. It requires a binary changed mask equal to the bitwise RGB delta,
// and forbids edits or generated support outside target and inside protection.
// Reader ownership stays with the caller. Every stream is hashed to completion
// before a content-classification failure is returned.
func VerifyRestorationPixelDelta(ctx context.Context, source, output, target,
	protected, changed PinnedMask, generated *PinnedMask,
	width, height int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return 0, ErrInvalid
	}
	src, err := openRestorationRGB(ctx, source, width, height)
	if err != nil {
		return 0, err
	}
	out, err := openRestorationRGB(ctx, output, width, height)
	if err != nil {
		return 0, err
	}
	tgt, err := openMaskPayload(ctx, target, width, height)
	if err != nil {
		return 0, err
	}
	guard, err := openMaskPayload(ctx, protected, width, height)
	if err != nil {
		return 0, err
	}
	delta, err := openMaskPayload(ctx, changed, width, height)
	if err != nil {
		return 0, err
	}
	var gen maskPayload
	if generated != nil {
		gen, err = openMaskPayload(ctx, *generated, width, height)
		if err != nil {
			return 0, err
		}
	}
	var sourceBlock, outputBlock [6144]byte
	var targetBlock, guardBlock, deltaBlock, generatedBlock [2048]byte
	changedCount := int64(0)
	violation := false
	for left := width * height; left > 0; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		pixels := int64(512)
		if left < pixels {
			pixels = left
		}
		rgbBytes := int(pixels * 12)
		maskBytes := int(pixels * 4)
		for _, piece := range []struct {
			reader io.Reader
			body   []byte
		}{
			{src.reader, sourceBlock[:rgbBytes]}, {out.reader, outputBlock[:rgbBytes]},
			{tgt.reader, targetBlock[:maskBytes]}, {guard.reader, guardBlock[:maskBytes]},
			{delta.reader, deltaBlock[:maskBytes]}} {
			if err := readUserMaskBytes(ctx, piece.reader, piece.body); err != nil {
				return 0, err
			}
		}
		if generated != nil {
			if err := readUserMaskBytes(ctx, gen.reader, generatedBlock[:maskBytes]); err != nil {
				return 0, err
			}
		}
		for i := 0; i < int(pixels); i++ {
			targetValue := math.Float32frombits(binary.LittleEndian.Uint32(targetBlock[i*4:]))
			protectedValue := math.Float32frombits(binary.LittleEndian.Uint32(guardBlock[i*4:]))
			changedValue := math.Float32frombits(binary.LittleEndian.Uint32(deltaBlock[i*4:]))
			if !(targetValue >= 0 && targetValue <= 1 && protectedValue >= 0 && protectedValue <= 1) ||
				(changedValue != 0 && changedValue != 1) {
				violation = true
			}
			pixelChanged := false
			for c := 0; c < 3; c++ {
				offset := i*12 + c*4
				beforeBits := binary.LittleEndian.Uint32(sourceBlock[offset:])
				afterBits := binary.LittleEndian.Uint32(outputBlock[offset:])
				if !finiteRestorationFloat(beforeBits) || !finiteRestorationFloat(afterBits) {
					violation = true
				}
				if beforeBits != afterBits {
					pixelChanged = true
				}
			}
			if pixelChanged {
				changedCount++
			}
			if pixelChanged != (changedValue == 1) ||
				(pixelChanged && (targetValue <= 0 || protectedValue > 0)) {
				violation = true
			}
			if generated != nil {
				g := math.Float32frombits(binary.LittleEndian.Uint32(generatedBlock[i*4:]))
				if (g != 0 && g != 1) || (g == 1 && (targetValue <= 0 || protectedValue > 0)) {
					violation = true
				}
			}
		}
		left -= pixels
	}
	for _, payload := range []maskPayload{src, out, tgt, guard, delta} {
		if err := finishMaskPayload(ctx, payload); err != nil {
			return 0, err
		}
	}
	if generated != nil {
		if err := finishMaskPayload(ctx, gen); err != nil {
			return 0, err
		}
	}
	if violation {
		return 0, ErrInvalid
	}
	return changedCount, nil
}

func finiteRestorationFloat(bits uint32) bool {
	value := math.Float32frombits(bits)
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

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

type identityAlpha struct {
	payload maskPayload
	uint16  bool
}

func openIdentityAlpha(ctx context.Context, pin PinnedMask, width, height int64) (identityAlpha, error) {
	var zero identityAlpha
	digest, err := hex.DecodeString(pin.SHA256)
	if pin.Reader == nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != pin.SHA256 ||
		pin.Bytes < 2*width*height+10 || pin.Bytes > 4*width*height+65548 {
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
	if headerBytes < 1 || headerBytes > 65536 {
		return zero, ErrInvalid
	}
	header := make([]byte, int(headerBytes))
	if err = readUserMaskBytes(ctx, r, header); err != nil {
		return zero, err
	}
	dimensions := []int64{height, width}
	uint16Payload := userMaskHeaderShape(header, dimensions, "<u2")
	if !uint16Payload && !userMaskHeaderShape(header, dimensions, "<f4") {
		return zero, ErrInvalid
	}
	componentBytes := int64(4)
	if uint16Payload {
		componentBytes = 2
	}
	if int64(8+lengthSize)+headerBytes+componentBytes*width*height != pin.Bytes {
		return zero, ErrInvalid
	}
	return identityAlpha{payload: maskPayload{r, h, digest}, uint16: uint16Payload}, nil
}

// IdentityPixelMeasurements are recomputed from exact before/after streams.
// A positive guard error is evidence for a fail report, not a verifier error.
type IdentityPixelMeasurements struct {
	ChangedPixels       int64
	OutsideMaskMaxError float64
	ProtectedMaxError   float64
}

// VerifyIdentityPixelDelta streams source/output RGB, effective/protected
// masks, changed-mask output and optional opacity. RGB change is bit-exact;
// alpha change uses the worker's float32 uint16 normalization and comparison.
// Every stream is consumed and SHA-checked before a value mismatch is returned.
func VerifyIdentityPixelDelta(ctx context.Context, before, after, effective, protected, changed PinnedMask,
	beforeAlpha, afterAlpha *PinnedMask, width, height int64) (IdentityPixelMeasurements, error) {
	var zero IdentityPixelMeasurements
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return zero, ErrInvalid
	}
	left, err := openRestorationRGB(ctx, before, width, height)
	if err != nil {
		return zero, err
	}
	right, err := openRestorationRGB(ctx, after, width, height)
	if err != nil {
		return zero, err
	}
	allowed, err := openMaskPayload(ctx, effective, width, height)
	if err != nil {
		return zero, err
	}
	guard, err := openMaskPayload(ctx, protected, width, height)
	if err != nil {
		return zero, err
	}
	delta, err := openMaskPayload(ctx, changed, width, height)
	if err != nil {
		return zero, err
	}
	var alphaLeft, alphaRight *identityAlpha
	if beforeAlpha != nil {
		a, e := openIdentityAlpha(ctx, *beforeAlpha, width, height)
		if e != nil {
			return zero, e
		}
		alphaLeft = &a
	}
	if afterAlpha != nil {
		a, e := openIdentityAlpha(ctx, *afterAlpha, width, height)
		if e != nil {
			return zero, e
		}
		alphaRight = &a
	}
	var leftBlock, rightBlock [6144]byte
	var allowedBlock, guardBlock, deltaBlock, alphaLeftBlock, alphaRightBlock [2048]byte
	measurements := IdentityPixelMeasurements{}
	violation := false
	for remaining := width * height; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		pixels := int64(512)
		if remaining < pixels {
			pixels = remaining
		}
		rgbBytes, maskBytes := int(pixels*12), int(pixels*4)
		for _, piece := range []struct {
			reader io.Reader
			body   []byte
		}{
			{left.reader, leftBlock[:rgbBytes]}, {right.reader, rightBlock[:rgbBytes]},
			{allowed.reader, allowedBlock[:maskBytes]}, {guard.reader, guardBlock[:maskBytes]},
			{delta.reader, deltaBlock[:maskBytes]}} {
			if err := readUserMaskBytes(ctx, piece.reader, piece.body); err != nil {
				return zero, err
			}
		}
		alphaRead := func(alpha *identityAlpha, body []byte) error {
			if alpha == nil {
				return nil
			}
			size := maskBytes
			if alpha.uint16 {
				size = int(pixels * 2)
			}
			return readUserMaskBytes(ctx, alpha.payload.reader, body[:size])
		}
		if err := alphaRead(alphaLeft, alphaLeftBlock[:]); err != nil {
			return zero, err
		}
		if err := alphaRead(alphaRight, alphaRightBlock[:]); err != nil {
			return zero, err
		}
		alphaValue := func(alpha *identityAlpha, body []byte, index int) float32 {
			if alpha == nil {
				return 1
			}
			if alpha.uint16 {
				return float32(binary.LittleEndian.Uint16(body[index*2:])) / float32(65535)
			}
			return math.Float32frombits(binary.LittleEndian.Uint32(body[index*4:]))
		}
		for i := 0; i < int(pixels); i++ {
			effectiveValue := math.Float32frombits(binary.LittleEndian.Uint32(allowedBlock[i*4:]))
			protectedValue := math.Float32frombits(binary.LittleEndian.Uint32(guardBlock[i*4:]))
			changedValue := math.Float32frombits(binary.LittleEndian.Uint32(deltaBlock[i*4:]))
			if !(effectiveValue >= 0 && effectiveValue <= 1 && protectedValue >= 0 && protectedValue <= 1) ||
				(changedValue != 0 && changedValue != 1) {
				violation = true
			}
			pixelChanged := false
			difference := float64(0)
			for channel := 0; channel < 3; channel++ {
				offset := i*12 + channel*4
				beforeBits := binary.LittleEndian.Uint32(leftBlock[offset:])
				afterBits := binary.LittleEndian.Uint32(rightBlock[offset:])
				if !finiteRestorationFloat(beforeBits) || !finiteRestorationFloat(afterBits) {
					violation = true
				}
				if beforeBits != afterBits {
					pixelChanged = true
				}
				value := math.Abs(float64(math.Float32frombits(beforeBits)) - float64(math.Float32frombits(afterBits)))
				if value > difference {
					difference = value
				}
			}
			beforeOpacity := alphaValue(alphaLeft, alphaLeftBlock[:], i)
			afterOpacity := alphaValue(alphaRight, alphaRightBlock[:], i)
			if !(beforeOpacity >= 0 && beforeOpacity <= 1 && afterOpacity >= 0 && afterOpacity <= 1) {
				violation = true
			}
			if beforeOpacity != afterOpacity {
				pixelChanged = true
			}
			opacityDifference := math.Abs(float64(beforeOpacity) - float64(afterOpacity))
			if opacityDifference > difference {
				difference = opacityDifference
			}
			if pixelChanged {
				measurements.ChangedPixels++
			}
			if pixelChanged != (changedValue == 1) {
				violation = true
			}
			if effectiveValue == 0 && difference > measurements.OutsideMaskMaxError {
				measurements.OutsideMaskMaxError = difference
			}
			if protectedValue > 0 && difference > measurements.ProtectedMaxError {
				measurements.ProtectedMaxError = difference
			}
		}
		remaining -= pixels
	}
	for _, payload := range []maskPayload{left, right, allowed, guard, delta} {
		if err := finishMaskPayload(ctx, payload); err != nil {
			return zero, err
		}
	}
	for _, alpha := range []*identityAlpha{alphaLeft, alphaRight} {
		if alpha != nil {
			if err := finishMaskPayload(ctx, alpha.payload); err != nil {
				return zero, err
			}
		}
	}
	if violation {
		return zero, ErrInvalid
	}
	return measurements, ctx.Err()
}

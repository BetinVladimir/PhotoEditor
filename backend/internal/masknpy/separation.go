package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"io"
	"math"
)

// PinnedMask identifies an exact stream; callers own and close Reader.
type PinnedMask struct {
	Reader io.Reader
	Bytes  int64
	SHA256 string
}
type maskPayload struct {
	reader io.Reader
	hash   hash.Hash
	digest []byte
}

func openMaskPayload(ctx context.Context, pin PinnedMask, width, height int64) (maskPayload, error) {
	return openTypedPayload(ctx, pin, width, height, "<f4", 4)
}

func openTypedPayload(ctx context.Context, pin PinnedMask, width, height int64, dtype string, componentBytes int64) (maskPayload, error) {
	empty := maskPayload{}
	digest, err := hex.DecodeString(pin.SHA256)
	if pin.Reader == nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != pin.SHA256 {
		return empty, ErrInvalid
	}
	dataBytes := componentBytes * width * height
	if pin.Bytes < dataBytes+10 || pin.Bytes > dataBytes+65548 {
		return empty, ErrInvalid
	}
	h := sha256.New()
	r := io.TeeReader(pin.Reader, h)
	var prefix [8]byte
	if err := readUserMaskBytes(ctx, r, prefix[:]); err != nil {
		return empty, err
	}
	if !bytes.Equal(prefix[:6], []byte{0x93, 'N', 'U', 'M', 'P', 'Y'}) || prefix[7] != 0 {
		return empty, ErrInvalid
	}
	lengthSize := 2
	switch prefix[6] {
	case 1:
	case 2, 3:
		lengthSize = 4
	default:
		return empty, ErrInvalid
	}
	var length [4]byte
	if err := readUserMaskBytes(ctx, r, length[:lengthSize]); err != nil {
		return empty, err
	}
	headerBytes := int64(binary.LittleEndian.Uint16(length[:2]))
	if lengthSize == 4 {
		headerBytes = int64(binary.LittleEndian.Uint32(length[:]))
	}
	if headerBytes < 1 || headerBytes > 65536 || int64(8+lengthSize)+headerBytes+dataBytes != pin.Bytes {
		return empty, ErrInvalid
	}
	header := make([]byte, int(headerBytes))
	if err := readUserMaskBytes(ctx, r, header); err != nil {
		return empty, err
	}
	if !userMaskHeaderShape(header, []int64{height, width}, dtype) {
		return empty, ErrInvalid
	}
	return maskPayload{r, h, digest}, nil
}
func finishMaskPayload(ctx context.Context, payload maskPayload) error {
	var tail [1]byte
	n, err := io.ReadFull(payload.reader, tail[:])
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	if n != 0 || err != io.EOF || !bytes.Equal(payload.hash.Sum(nil), payload.digest) {
		return ErrInvalid
	}
	return nil
}

// VerifyMaskSeparation verifies both complete pinned NPY objects in lockstep,
// requiring a nonempty selected mask and zero positive protected overlap.
// Two64KiB buffers suffice regardless of dimensions; headers may differ.
// No mask values are cleared or transformed. All refusals return zero count.
func VerifyMaskSeparation(ctx context.Context, selected, protected PinnedMask, width, height int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return 0, ErrInvalid
	}
	a, err := openMaskPayload(ctx, selected, width, height)
	if err != nil {
		return 0, err
	}
	b, err := openMaskPayload(ctx, protected, width, height)
	if err != nil {
		return 0, err
	}
	var left, right [65536]byte
	nonempty := int64(0)
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
			if !(x >= 0 && x <= 1 && y >= 0 && y <= 1) || (x > 0 && y > 0) {
				return 0, ErrInvalid
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
	if nonempty == 0 {
		return 0, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return nonempty, nil
}

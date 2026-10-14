package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var ErrInvalid = errors.New("invalid float32 mask NPY")

var maskHeaderField = regexp.MustCompile(`^\s*['"]([a-z_]+)['"]\s*:\s*('[^']*'|"[^"]*"|False|True|\([^)]*\))\s*`)
var maskHeaderShape = regexp.MustCompile(`^\(\s*[1-9][0-9]*(\s*,\s*[1-9][0-9]*)+\s*,?\s*\)$`)

func readUserMaskBytes(ctx context.Context, reader io.Reader, buffer []byte) error {
	_, err := io.ReadFull(reader, buffer)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return ErrInvalid
	}
	return err
}

func userMaskHeaderShape(header []byte, dimensions []int64, dtype string) bool {
	if len(header) == 0 || header[len(header)-1] != '\n' {
		return false
	}
	text := strings.TrimSpace(string(header))
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return false
	}
	text = text[1 : len(text)-1]
	fields := map[string]string{}
	for strings.TrimSpace(text) != "" {
		found := maskHeaderField.FindStringSubmatchIndex(text)
		if found == nil {
			return false
		}
		key, value := text[found[2]:found[3]], text[found[4]:found[5]]
		if _, duplicate := fields[key]; duplicate {
			return false
		}
		fields[key] = value
		text = text[found[1]:]
		if strings.TrimSpace(text) == "" {
			break
		}
		text = strings.TrimLeft(text, " \t\r\n")
		if text[0] != ',' {
			return false
		}
		text = text[1:]
	}
	if len(fields) != 3 || fields["fortran_order"] != "False" || (fields["descr"] != "'"+dtype+"'" && fields["descr"] != "\""+dtype+"\"") {
		return false
	}
	shape := fields["shape"]
	if !maskHeaderShape.MatchString(shape) {
		return false
	}
	parts := strings.Split(strings.TrimSpace(shape)[1:len(strings.TrimSpace(shape))-1], ",")
	if strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) != len(dimensions) {
		return false
	}
	for i, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || value != dimensions[i] {
			return false
		}
	}
	return true
}

// VerifyFloat32MaskNPY reads a pinned object with bounded memory. Only two-
// dimensional C-order little-endian float32 probabilities are accepted; pickle,
// structured/object arrays and Python expressions cannot enter the parser.
// The caller owns/ closes reader, including on early validation failure.
func VerifyFloat32MaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	return VerifyFloat32ArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 0, true)
}

// VerifyFloat32ArrayNPY accepts a 2D array (channels=0) or HWC array
// (channels=2 or 3). All values must be finite; unit also requires [0,1].
// Memory is bounded independently of image dimensions; caller closes reader.
func VerifyFloat32ArrayNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64, channels int, unit bool) error {
	return verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, channels, unit, false, false, false, nil, false)
}

// VerifyUint16RGBNPY accepts only little-endian C-order HWC uint16 RGB.
func VerifyUint16RGBNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	return verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 3, false, false, false, false, nil, true)
}

// VerifyAlphaNPY accepts source opacity as little-endian uint16 or unit float32.
// It verifies bytes only; uint16 normalization stays in the existing worker.
func VerifyAlphaNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	return verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 0, true, true, false, false, nil, false)
}

func verifyArrayNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64, channels int, unit, legacyAlpha, binaryMask, fullMask bool, sample func(int64, float32) bool, uint16Only bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := hex.DecodeString(expectedSHA)
	if reader == nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != expectedSHA || width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return ErrInvalid
	}
	dimensions := []int64{height, width}
	components := int64(1)
	if channels != 0 {
		if channels != 2 && channels != 3 {
			return ErrInvalid
		}
		components = int64(channels)
		dimensions = append(dimensions, components)
	}
	pixels := width * height
	dataBytes := 4 * pixels * components
	minimumDataBytes := dataBytes
	if legacyAlpha || uint16Only {
		minimumDataBytes = 2 * pixels * components
	}
	if expectedBytes < minimumDataBytes+10 || expectedBytes > dataBytes+65548 {
		return ErrInvalid
	}
	hash := sha256.New()
	stream := io.TeeReader(reader, hash)
	var prefix [8]byte
	if err := readUserMaskBytes(ctx, stream, prefix[:]); err != nil {
		return err
	}
	if !bytes.Equal(prefix[:6], []byte{0x93, 'N', 'U', 'M', 'P', 'Y'}) || prefix[7] != 0 {
		return ErrInvalid
	}
	var length [4]byte
	size := 2
	switch prefix[6] {
	case 1:
	case 2, 3:
		size = 4
	default:
		return ErrInvalid
	}
	if err := readUserMaskBytes(ctx, stream, length[:size]); err != nil {
		return err
	}
	headerSize := int64(binary.LittleEndian.Uint16(length[:2]))
	if size == 4 {
		headerSize = int64(binary.LittleEndian.Uint32(length[:]))
	}
	if headerSize < 1 || headerSize > 65536 {
		return ErrInvalid
	}
	header := make([]byte, int(headerSize))
	if err := readUserMaskBytes(ctx, stream, header); err != nil {
		return err
	}
	uint16Payload := (legacyAlpha || uint16Only) && userMaskHeaderShape(header, dimensions, "<u2")
	if uint16Payload {
		dataBytes = 2 * pixels * components
	}
	if int64(8+size)+headerSize+dataBytes != expectedBytes ||
		(uint16Only && !uint16Payload) || (!uint16Payload && !userMaskHeaderShape(header, dimensions, "<f4")) {
		return ErrInvalid
	}
	var buffer [65536]byte
	for remaining := dataBytes; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := int64(len(buffer))
		if remaining < count {
			count = remaining
		}
		if err := readUserMaskBytes(ctx, stream, buffer[:int(count)]); err != nil {
			return err
		}
		for i := 0; !uint16Payload && i < int(count); i += 4 {
			value := math.Float32frombits(binary.LittleEndian.Uint32(buffer[i : i+4]))
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || (unit && !(value >= 0 && value <= 1)) || (binaryMask && value != 0 && value != 1) || (fullMask && value != 1) {
				return ErrInvalid
			}
			if sample != nil && !sample((dataBytes-remaining+int64(i))/4, value) {
				return ErrInvalid
			}
		}
		remaining -= count
	}
	var tail [1]byte
	n, err := io.ReadFull(stream, tail[:])
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	if n != 0 || err != io.EOF {
		return ErrInvalid
	}
	if !bytes.Equal(hash.Sum(nil), digest) {
		return ErrInvalid
	}
	return ctx.Err()
}

// VerifyBinaryMaskNPY requires exact finite float32 zero/one values.
func VerifyBinaryMaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	return verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 0, true, false, true, false, nil, false)
}

// VerifyFullMaskNPY requires every float32 mask sample to be exactly one.
// Geometry-changing operations must mark the full output as changed.
func VerifyFullMaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	return verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 0, true, false, true, true, nil, false)
}

// CountBinaryMaskNPY exposes a count only after complete shape/value/length/SHA
// validation. It streams with the same bounded buffer as VerifyBinaryMaskNPY.
// Partial counts on cancellation, transport failure or a bad tail are discarded.
func CountBinaryMaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) (int64, error) {
	var count int64
	err := verifyArrayNPY(ctx, reader, expectedBytes, expectedSHA, width, height, 0, true, false, true, false, func(_ int64, value float32) bool {
		if value == 1 {
			count++
		}
		return true
	}, false)
	if err != nil {
		return 0, err
	}
	return count, nil
}

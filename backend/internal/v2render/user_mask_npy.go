package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"photoeditor/backend/internal/v2input"
	"regexp"
	"strconv"
	"strings"
)

var maskHeaderField = regexp.MustCompile(`^\s*['"]([a-z_]+)['"]\s*:\s*('[^']*'|"[^"]*"|False|True|\([^)]*\))\s*`)
var maskHeaderShape = regexp.MustCompile(`^\(\s*([1-9][0-9]*)\s*,\s*([1-9][0-9]*)\s*,?\s*\)$`)

func readUserMaskBytes(ctx context.Context, reader io.Reader, buffer []byte) error {
	_, err := io.ReadFull(reader, buffer)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return v2input.ErrInvalid
	}
	return err
}

func userMaskHeaderShape(header []byte, width, height int64) bool {
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
	if len(fields) != 3 || fields["fortran_order"] != "False" || (fields["descr"] != "'<f4'" && fields["descr"] != "\"<f4\"") {
		return false
	}
	shape := maskHeaderShape.FindStringSubmatch(fields["shape"])
	if shape == nil {
		return false
	}
	rows, err := strconv.ParseInt(shape[1], 10, 64)
	if err != nil || rows != height {
		return false
	}
	cols, err := strconv.ParseInt(shape[2], 10, 64)
	return err == nil && cols == width
}

// VerifyUserMaskNPY reads a pinned object with bounded memory. Only two-
// dimensional C-order little-endian float32 probabilities are accepted; pickle,
// structured/object arrays and Python expressions cannot enter the parser.
// The caller owns/ closes reader, including on early validation failure.
func VerifyUserMaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := hex.DecodeString(expectedSHA)
	if reader == nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != expectedSHA || width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return v2input.ErrInvalid
	}
	pixels := width * height
	dataBytes := 4 * pixels
	if expectedBytes < dataBytes+10 || expectedBytes > dataBytes+65548 {
		return v2input.ErrInvalid
	}
	hash := sha256.New()
	stream := io.TeeReader(reader, hash)
	var prefix [8]byte
	if err := readUserMaskBytes(ctx, stream, prefix[:]); err != nil {
		return err
	}
	if !bytes.Equal(prefix[:6], []byte{0x93, 'N', 'U', 'M', 'P', 'Y'}) || prefix[7] != 0 {
		return v2input.ErrInvalid
	}
	var length [4]byte
	size := 2
	switch prefix[6] {
	case 1:
	case 2, 3:
		size = 4
	default:
		return v2input.ErrInvalid
	}
	if err := readUserMaskBytes(ctx, stream, length[:size]); err != nil {
		return err
	}
	headerSize := int64(binary.LittleEndian.Uint16(length[:2]))
	if size == 4 {
		headerSize = int64(binary.LittleEndian.Uint32(length[:]))
	}
	if headerSize < 1 || headerSize > 65536 || int64(8+size)+headerSize+dataBytes != expectedBytes {
		return v2input.ErrInvalid
	}
	header := make([]byte, int(headerSize))
	if err := readUserMaskBytes(ctx, stream, header); err != nil {
		return err
	}
	if !userMaskHeaderShape(header, width, height) {
		return v2input.ErrInvalid
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
		for i := 0; i < int(count); i += 4 {
			value := math.Float32frombits(binary.LittleEndian.Uint32(buffer[i : i+4]))
			if !(value >= 0 && value <= 1) {
				return v2input.ErrInvalid
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
		return v2input.ErrInvalid
	}
	if !bytes.Equal(hash.Sum(nil), digest) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

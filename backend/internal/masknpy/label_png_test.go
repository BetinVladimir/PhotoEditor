package masknpy

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"
)

func testLabelPNG(values []uint16, width, height int, filter byte) []byte {
	result := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	chunk := func(kind string, data []byte) {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))
		result = append(result, prefix[:]...)
		result = append(result, []byte(kind)...)
		result = append(result, data...)
		binary.BigEndian.PutUint32(prefix[:], crc32.ChecksumIEEE(append([]byte(kind), data...)))
		result = append(result, prefix[:]...)
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[:4], uint32(width))
	binary.BigEndian.PutUint32(header[4:8], uint32(height))
	header[8] = 16
	chunk("IHDR", header)
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	for y := 0; y < height; y++ {
		_, _ = writer.Write([]byte{filter})
		for x := 0; x < width; x++ {
			var b [2]byte
			binary.BigEndian.PutUint16(b[:], values[y*width+x])
			_, _ = writer.Write(b[:])
		}
	}
	_ = writer.Close()
	data := compressed.Bytes()
	// Small chunks also exercise the compressed stream crossing IDAT CRCs.
	for len(data) > 0 {
		n := 7
		if len(data) < n {
			n = len(data)
		}
		chunk("IDAT", data[:n])
		data = data[n:]
	}
	chunk("IEND", nil)
	return result
}
func TestDefectLabelPNGExactAndIntegrity(t *testing.T) {
	values := []uint16{0, 7, 65535, 1, 0, 256}
	labels := labelFixture(values)
	png := testLabelPNG(values, 3, 2, 0)
	if err := VerifyDefectLabelPNG(context.Background(), labelPin(labels), labelPin(png), 3, 2); err != nil {
		t.Fatal(err)
	}
	changedValues := append([]uint16(nil), values...)
	changedValues[5]++
	cases := [][]byte{testLabelPNG(changedValues, 3, 2, 0), testLabelPNG(values, 3, 2, 1), testLabelPNG(values, 2, 3, 0), png[:len(png)-1], append(append([]byte{}, png...), 0)}
	badCRC := append([]byte{}, png...)
	badCRC[len(badCRC)-1] ^= 1
	cases = append(cases, badCRC)
	badIDAT := append([]byte{}, png...)
	badIDAT[41] ^= 1
	cases = append(cases, badIDAT)
	for i, body := range cases {
		if err := VerifyDefectLabelPNG(context.Background(), labelPin(labels), labelPin(body), 3, 2); err == nil {
			t.Fatal("invalid PNG accepted", i)
		}
	}
	for _, side := range []string{"png", "npy"} {
		a, b := labelPin(labels), labelPin(png)
		if side == "png" {
			b.SHA256 = string(bytes.Repeat([]byte{'0'}, 64))
		} else {
			a.SHA256 = string(bytes.Repeat([]byte{'0'}, 64))
		}
		if err := VerifyDefectLabelPNG(context.Background(), a, b, 3, 2); err == nil {
			t.Fatal("wrong SHA accepted", side)
		}
	}
	pin := labelPin(png)
	pin.Bytes++
	if err := VerifyDefectLabelPNG(context.Background(), labelPin(labels), pin, 3, 2); err == nil {
		t.Fatal("wrong byte count accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyDefectLabelPNG(ctx, labelPin(labels), labelPin(png), 3, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

func labelFixture(values []uint16) []byte {
	header := []byte("{'descr':'<u2','fortran_order':False,'shape':(2,3)}\n")
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, header...)
	for _, v := range values {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], v)
		body = append(body, b[:]...)
	}
	return body
}
func labelPin(body []byte) PinnedMask {
	sum := sha256.Sum256(body)
	return PinnedMask{bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:])}
}
func TestDefectLabelsPhysicalCandidateSetAndBounds(t *testing.T) {
	candidates := []LabelBounds{{Label: 7, X: 1, Y: 0, Width: 2, Height: 2}, {Label: 65535, X: 0, Y: 1, Width: 1, Height: 1}}
	body := labelFixture([]uint16{0, 7, 7, 65535, 0, 7})
	evidence, err := VerifyDefectLabelsNPY(context.Background(), labelPin(body), 3, 2, candidates)
	if err != nil || len(evidence) != 2 || evidence[0].Pixels != 3 || evidence[1].Pixels != 1 {
		t.Fatal(evidence, err)
	}
	tests := []struct {
		name   string
		body   []byte
		bounds []LabelBounds
	}{
		{"unknown", labelFixture([]uint16{1, 7, 7, 65535, 0, 7}), candidates},
		{"missing", labelFixture([]uint16{0, 7, 7, 0, 0, 7}), candidates},
		{"wrong bounds", labelFixture([]uint16{7, 7, 7, 65535, 0, 7}), candidates},
		{"duplicate", body, append(append([]LabelBounds{}, candidates...), candidates[0])},
		{"zero label", body, []LabelBounds{{Label: 0, Width: 1, Height: 1}}},
		{"outside canvas", body, []LabelBounds{{Label: 7, X: 3, Width: 1, Height: 1}}},
		{"trailing", append(append([]byte{}, body...), 0), candidates},
		{"truncated", body[:len(body)-1], candidates},
		{"float32", arrayFixture("(2,3)", []float32{0, 1, 1, 0, 0, 1}), candidates},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := VerifyDefectLabelsNPY(context.Background(), labelPin(tc.body), 3, 2, tc.bounds)
			if err == nil || result != nil {
				t.Fatal(result, err)
			}
		})
	}
	pin := labelPin(body)
	pin.SHA256 = hex.EncodeToString(make([]byte, 32))
	if result, err := VerifyDefectLabelsNPY(context.Background(), pin, 3, 2, candidates); err == nil || result != nil {
		t.Fatal("wrong SHA accepted")
	}
	if result, err := VerifyDefectLabelsNPY(context.Background(), labelPin(body), 2, 3, candidates); err == nil || result != nil {
		t.Fatal("wrong dimensions accepted")
	}
	if result, err := VerifyDefectLabelsNPY(context.Background(), labelPin(labelFixture(make([]uint16, 6))), 3, 2, nil); err != nil || len(result) != 0 {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := VerifyDefectLabelsNPY(ctx, labelPin(body), 3, 2, candidates); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal(result, err)
	}
}

func TestDefectLabelsAcrossStreamChunks(t *testing.T) {
	header := []byte("{'descr':'<u2','fortran_order':False,'shape':(300,400)}\n")
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, header...)
	offset := len(body)
	body = append(body, make([]byte, 2*300*400)...)
	// A sparse label spans multiple 64KiB blocks and the last pixel.
	for _, index := range []int{0, 32768, 119999} {
		binary.LittleEndian.PutUint16(body[offset+2*index:], 9)
	}
	bounds := []LabelBounds{{Label: 9, Width: 400, Height: 300}}
	result, err := VerifyDefectLabelsNPY(context.Background(), labelPin(body), 400, 300, bounds)
	if err != nil || len(result) != 1 || result[0].Pixels != 3 {
		t.Fatal(result, err)
	}
	body[offset+2*119998] = 1
	if result, err := VerifyDefectLabelsNPY(context.Background(), labelPin(body), 400, 300, bounds); err == nil || result != nil {
		t.Fatal("unknown label in final block accepted", result, err)
	}
}

package masknpy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestDefectLabelsProtectionOriginalStreams(t *testing.T) {
	labels := labelFixture([]uint16{0, 7, 7, 0, 0, 7})
	bounds := []LabelBounds{{Label: 7, X: 1, Y: 0, Width: 2, Height: 2}}
	valid := arrayFixture("(2,3)", []float32{1, 0, 0, .5, 0, 0})
	result, err := VerifyDefectLabelsProtectionNPY(context.Background(), labelPin(labels), pinnedMask(valid), 3, 2, bounds)
	if err != nil || len(result) != 1 || result[0].Pixels != 3 {
		t.Fatal(result, err)
	}
	for _, values := range [][]float32{
		{1, 1, 0, .5, 0, 0}, {1, .001, 0, .5, 0, 0}, {1, 0, 0, .5, 0, math.SmallestNonzeroFloat32},
		{1, 0, 0, .5, float32(math.NaN()), 0}, {1, 0, 0, .5, float32(math.Inf(1)), 0}, {1, 0, 0, -.01, 0, 0}, {1, 0, 0, 1.01, 0, 0},
	} {
		if got, err := VerifyDefectLabelsProtectionNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(2,3)", values)), 3, 2, bounds); err == nil || got != nil {
			t.Fatal("invalid protection accepted", got, err)
		}
	}
	for _, side := range []string{"labels", "protection"} {
		for _, corruption := range []string{"truncated", "trailing", "sha"} {
			a, b := labelPin(labels), pinnedMask(valid)
			pin, body := &a, labels
			if side == "protection" {
				pin, body = &b, valid
			}
			switch corruption {
			case "truncated":
				*pin = labelPin(body[:len(body)-1])
			case "trailing":
				*pin = labelPin(append(append([]byte{}, body...), 0))
			case "sha":
				pin.SHA256 = string(bytes.Repeat([]byte{'0'}, 64))
			}
			if got, err := VerifyDefectLabelsProtectionNPY(context.Background(), a, b, 3, 2, bounds); err == nil || got != nil {
				t.Fatal(side, corruption, got, err)
			}
		}
	}
	empty := labelFixture(make([]uint16, 6))
	if got, err := VerifyDefectLabelsProtectionNPY(context.Background(), labelPin(empty), pinnedMask(valid), 3, 2, nil); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := VerifyDefectLabelsProtectionNPY(ctx, labelPin(labels), pinnedMask(valid), 3, 2, bounds); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal(got, err)
	}
}

func TestDefectLabelsProtectionChunkBoundary(t *testing.T) {
	header := []byte("{'descr':'<u2','fortran_order':False,'shape':(1,20000)}\n")
	labels := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(labels[8:], uint16(len(header)))
	labels = append(labels, header...)
	payload := make([]byte, 40000)
	for _, position := range []int{0, 16383, 16384, 19999} {
		binary.LittleEndian.PutUint16(payload[position*2:], 9)
	}
	labels = append(labels, payload...)
	bounds := []LabelBounds{{Label: 9, Width: 20000, Height: 1}}
	values := make([]float32, 20000)
	if got, err := VerifyDefectLabelsProtectionNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(1,20000)", values)), 20000, 1, bounds); err != nil || len(got) != 1 || got[0].Pixels != 4 {
		t.Fatal(got, err)
	}
	for _, position := range []int{16383, 16384, 19999} {
		values[position] = .01
		if got, err := VerifyDefectLabelsProtectionNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(1,20000)", values)), 20000, 1, bounds); err == nil || got != nil {
			t.Fatal(position, got, err)
		}
		values[position] = 0
	}
}

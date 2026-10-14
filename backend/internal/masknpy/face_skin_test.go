package masknpy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestFaceSkinExactLabelsAndIntegrity(t *testing.T) {
	labels := labelFixture([]uint16{0, 1, 18, 1, 2, 9})
	skin := arrayFixture("(2,3)", []float32{0, 1, 0, 1, 0, 0})
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pinnedMask(skin), 3, 2); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, values := range [][]float32{{0, .5, 0, 1, 0, 0}, {0, 1, 1, 1, 0, 0}, {0, 1, 0, 1, 0, float32(math.NaN())}} {
		if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(2,3)", values)), 3, 2); err == nil || n != 0 {
			t.Fatal("invalid skin accepted", n, err)
		}
	}
	invalid := labelFixture([]uint16{0, 1, 19, 1, 2, 9})
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(invalid), pinnedMask(skin), 3, 2); err == nil || n != 0 {
		t.Fatal("invalid class accepted")
	}
	for _, bad := range [][]byte{skin[:len(skin)-1], append(append([]byte{}, skin...), 0)} {
		if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pinnedMask(bad), 3, 2); err == nil || n != 0 {
			t.Fatal("invalid size accepted")
		}
	}
	pin := pinnedMask(skin)
	changed := append([]byte{}, skin...)
	changed[len(changed)-1] ^= 1
	pin.Reader = bytes.NewReader(changed)
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pin, 3, 2); err == nil || n != 0 {
		t.Fatal("changed bytes accepted")
	}
	empty := labelFixture(make([]uint16, 6))
	zero := arrayFixture("(2,3)", make([]float32, 6))
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(empty), pinnedMask(zero), 3, 2); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := VerifyFaceSkinNPY(ctx, labelPin(labels), pinnedMask(skin), 3, 2); !errors.Is(err, context.Canceled) || n != 0 {
		t.Fatal(n, err)
	}
}

func TestFaceSkinChunkBoundary(t *testing.T) {
	header := []byte("{'descr':'<u2','fortran_order':False,'shape':(1,20000)}\n")
	labels := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(labels[8:], uint16(len(header)))
	labels = append(labels, header...)
	payload := make([]byte, 40000)
	values := make([]float32, 20000)
	for _, position := range []int{16383, 16384, 19999} {
		binary.LittleEndian.PutUint16(payload[position*2:], 1)
		values[position] = 1
	}
	labels = append(labels, payload...)
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(1,20000)", values)), 20000, 1); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	values[19999] = 0
	if n, err := VerifyFaceSkinNPY(context.Background(), labelPin(labels), pinnedMask(arrayFixture("(1,20000)", values)), 20000, 1); err == nil || n != 0 {
		t.Fatal("last pixel mismatch accepted", n, err)
	}
}

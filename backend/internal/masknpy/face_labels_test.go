package masknpy

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func TestFaceLabelsIndependentIntegrity(t *testing.T) {
	raw := labelFixture([]uint16{0, 1, 18, 1, 2, 9})
	want := [19]int64{}
	want[0] = 1
	want[1] = 2
	want[18] = 1
	want[2] = 1
	want[9] = 1
	if got, err := VerifyFaceLabelsNPY(context.Background(), labelPin(raw), 3, 2); err != nil || got != want {
		t.Fatal(got, err)
	}
	invalid := labelFixture([]uint16{0, 1, 19, 1, 2, 9})
	for _, body := range [][]byte{invalid, raw[:len(raw)-1], append(append([]byte{}, raw...), 0)} {
		if got, err := VerifyFaceLabelsNPY(context.Background(), labelPin(body), 3, 2); err == nil || got != ([19]int64{}) {
			t.Fatal("invalid labels leaked evidence", got, err)
		}
	}
	pin := labelPin(raw)
	changed := append([]byte{}, raw...)
	changed[len(changed)-2] = 18
	pin.Reader = bytes.NewReader(changed)
	if got, err := VerifyFaceLabelsNPY(context.Background(), pin, 3, 2); err == nil || got != ([19]int64{}) {
		t.Fatal("hash mismatch accepted", got, err)
	}
	header := []byte("{'descr':'<u2','fortran_order':False,'shape':(1,20000)}\n")
	large := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(large[8:], uint16(len(header)))
	large = append(large, header...)
	data := make([]byte, 40000)
	for _, i := range []int{16383, 16384, 19999} {
		binary.LittleEndian.PutUint16(data[i*2:], 18)
	}
	large = append(large, data...)
	if got, err := VerifyFaceLabelsNPY(context.Background(), labelPin(large), 20000, 1); err != nil || got[18] != 3 || got[0] != 19997 {
		t.Fatal("chunk boundary", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := VerifyFaceLabelsNPY(ctx, labelPin(raw), 3, 2); err != context.Canceled || got != ([19]int64{}) {
		t.Fatal("cancel", got, err)
	}
}

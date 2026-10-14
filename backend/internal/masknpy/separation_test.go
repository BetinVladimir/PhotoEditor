package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"testing"
)

func pinnedMask(raw []byte) PinnedMask {
	sum := sha256.Sum256(raw)
	return PinnedMask{Reader: bytes.NewReader(raw), Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
}
func TestMaskSeparationNonemptyOverlapIntegrityAndBoundaries(t *testing.T) {
	selected := arrayFixture("(2,3)", []float32{1, .5, 0, 0, 0, 0})
	protected := arrayFixture("(2,3)", []float32{0, 0, 1, 0, 0, 0})
	count, err := VerifyMaskSeparation(context.Background(), pinnedMask(selected), pinnedMask(protected), 3, 2)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	// Identical data shape with an independent NPY2 header layout is valid.
	version2 := append([]byte(nil), protected[:8]...)
	version2[6] = 2
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(binary.LittleEndian.Uint16(protected[8:10])))
	version2 = append(version2, length[:]...)
	version2 = append(version2, protected[10:]...)
	if count, err := VerifyMaskSeparation(context.Background(), pinnedMask(selected), pinnedMask(version2), 3, 2); err != nil || count != 2 {
		t.Fatal("different NPY headers", count, err)
	}
	for _, pair := range [][2][]byte{
		{arrayFixture("(2,3)", make([]float32, 6)), protected},
		{selected, arrayFixture("(2,3)", []float32{0, 1e-20, 0, 0, 0, 0})},
		{selected, arrayFixture("(2,3)", []float32{0, 0, 1.1, 0, 0, 0})},
		{arrayFixture("(2,3)", []float32{1, .5, 0, 0, 0, float32(math.NaN())}), protected},
		{selected, arrayFixture("(2,3)", []float32{0, 0, float32(math.Inf(1)), 0, 0, 0})},
		{selected, arrayFixture("(3,2)", make([]float32, 6))},
		{selected, protected[:len(protected)-1]},
		{append(append([]byte(nil), selected...), 0), protected},
	} {
		if count, err := VerifyMaskSeparation(context.Background(), pinnedMask(pair[0]), pinnedMask(pair[1]), 3, 2); err == nil || count != 0 {
			t.Fatal("invalid pair accepted", count, err)
		}
	}
	altered := append([]byte(nil), protected...)
	altered[len(altered)-1] ^= 1
	pin := pinnedMask(protected)
	pin.Reader = bytes.NewReader(altered)
	if count, err := VerifyMaskSeparation(context.Background(), pinnedMask(selected), pin, 3, 2); err == nil || count != 0 {
		t.Fatal("changed physical bytes accepted")
	}
	largeA, largeB := make([]float32, 20000), make([]float32, 20000)
	largeA[16384] = 1
	largeB[16383] = 1
	count, err = VerifyMaskSeparation(context.Background(), pinnedMask(arrayFixture("(1,20000)", largeA)), pinnedMask(arrayFixture("(1,20000)", largeB)), 20000, 1)
	if err != nil || count != 1 {
		t.Fatal("buffer boundary", count, err)
	}
	largeB[16384] = .01
	if count, err := VerifyMaskSeparation(context.Background(), pinnedMask(arrayFixture("(1,20000)", largeA)), pinnedMask(arrayFixture("(1,20000)", largeB)), 20000, 1); err == nil || count != 0 {
		t.Fatal("boundary overlap accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if count, err := VerifyMaskSeparation(ctx, pinnedMask(selected), pinnedMask(protected), 3, 2); !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatal("cancel lost")
	}
}

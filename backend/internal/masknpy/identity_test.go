package masknpy

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
)

func identityUint16Alpha(shape string, values []uint16) []byte {
	header := []byte(fmt.Sprintf("{'descr':'<u2','fortran_order':False,'shape':%s}\n", shape))
	body := make([]byte, 10+len(header)+2*len(values))
	copy(body, []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0})
	binary.LittleEndian.PutUint16(body[8:10], uint16(len(header)))
	copy(body[10:], header)
	for i, value := range values {
		binary.LittleEndian.PutUint16(body[10+len(header)+2*i:], value)
	}
	return body
}

func TestIdentityPixelDeltaRGBAlphaAndGuardMetrics(t *testing.T) {
	beforeRGB := arrayFixture("(2,2,3)", make([]float32, 12))
	afterValues := make([]float32, 12)
	afterValues[0] = .5
	afterRGB := arrayFixture("(2,2,3)", afterValues)
	effective := arrayFixture("(2,2)", []float32{0, 1, 1, 1})
	protected := arrayFixture("(2,2)", []float32{1, 0, 0, 0})
	changed := arrayFixture("(2,2)", []float32{1, 1, 0, 0})
	beforeAlpha := identityUint16Alpha("(2,2)", []uint16{65535, 65535, 65535, 65535})
	afterAlpha := arrayFixture("(2,2)", []float32{1, .5, 1, 1})
	verify := func(mask []byte) (IdentityPixelMeasurements, error) {
		left, right := pinnedMask(beforeAlpha), pinnedMask(afterAlpha)
		return VerifyIdentityPixelDelta(context.Background(), pinnedMask(beforeRGB), pinnedMask(afterRGB),
			pinnedMask(effective), pinnedMask(protected), pinnedMask(mask), &left, &right, 2, 2)
	}
	got, err := verify(changed)
	if err != nil || got.ChangedPixels != 2 || got.OutsideMaskMaxError != .5 || got.ProtectedMaxError != .5 {
		t.Fatal(got, err)
	}
	if _, err := verify(arrayFixture("(2,2)", []float32{1, 0, 0, 0})); err == nil {
		t.Fatal("missing alpha-only changed pixel accepted")
	}
	corrupt := append([]byte(nil), changed...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := verify(corrupt); err == nil {
		t.Fatal("changed-mask corruption accepted")
	}
}

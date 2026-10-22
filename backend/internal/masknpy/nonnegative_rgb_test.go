package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

func TestNonnegativeRGBPreservesHDRAndRejectsNegativeNonfinite(t *testing.T) {
	for _, v := range []float32{0, 2, -1, float32(math.Inf(1)), float32(math.NaN())} {
		header := []byte("{'descr': '<f4', 'fortran_order': False, 'shape': (1, 1, 3), }\n")
		header = header[:len(header)-1]
		for (10+len(header)+1)%64 != 0 {
			header = append(header, ' ')
		}
		header = append(header, '\n')
		raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, byte(len(header)), 0}
		raw = append(raw, header...)
		for i := 0; i < 3; i++ {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
			raw = append(raw, b[:]...)
		}
		sum := sha256.Sum256(raw)
		err := VerifyNonnegativeFloat32RGBNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), hex.EncodeToString(sum[:]), 1, 1)
		valid := v >= 0 && !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
		if (err == nil) != valid {
			t.Fatal(v, err)
		}
	}
}

package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
)

func TestBinaryMaskRejectsFractionalAndNonfinite(t *testing.T) {
	for _, value := range []float32{0, 1, .5, -1, 2, float32(math.NaN()), float32(math.Inf(1))} {
		raw := arrayFixture("(2,3)", []float32{0, 1, 0, 1, 0, value})
		err := VerifyBinaryMaskNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), arraySHAForBinary(raw), 3, 2)
		if (value == 0 || value == 1) != (err == nil) {
			t.Fatalf("value=%v err=%v", value, err)
		}
	}
}

func arraySHAForBinary(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestFullMaskRequiresEverySample(t *testing.T) {
	for _, last := range []float32{0, .5, 1} {
		raw := arrayFixture("(2,3)", []float32{1, 1, 1, 1, 1, last})
		err := VerifyFullMaskNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), arraySHAForBinary(raw), 3, 2)
		if (last == 1) != (err == nil) {
			t.Fatalf("last=%v error=%v", last, err)
		}
	}
}

func TestBinaryMaskCountRequiresCompletePinnedValidArray(t *testing.T) {
	for _, mode := range []string{"valid", "empty", "full", "fractional", "hash", "tail", "truncated", "shape", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			values := []float32{0, 1, 0, 1, 0, 1}
			want := int64(3)
			switch mode {
			case "empty":
				values = []float32{0, 0, 0, 0, 0, 0}
				want = 0
			case "full":
				values = []float32{1, 1, 1, 1, 1, 1}
				want = 6
			case "fractional":
				values[5] = .5
			}
			raw := arrayFixture("(2,3)", values)
			sha := arraySHAForBinary(raw)
			width := int64(3)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "hash":
				sha = arraySHAForBinary([]byte("different"))
			case "tail":
				raw = append(raw, 0)
			case "truncated":
				raw = raw[:len(raw)-1]
			case "shape":
				width = 2
			case "cancelled":
				cancel()
			}
			count, err := CountBinaryMaskNPY(ctx, bytes.NewReader(raw), int64(len(raw)), sha, width, 2)
			if mode == "valid" || mode == "empty" || mode == "full" {
				if err != nil || count != want {
					t.Fatal("wrong binary mask count", count, err)
				}
				return
			}
			if err == nil || count != 0 {
				t.Fatal("invalid array exposed partial count", count, err)
			}
		})
	}
}

package v2optical

import (
	"context"
	"errors"
	"math"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiHomographyNumericalSafety(t *testing.T) {
	cases := []struct {
		name   string
		matrix []float64
		valid  bool
	}{
		{"identity", []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, true},
		{"scale_invariant", []float64{2, 0, 0, 0, 2, 0, 0, 0, 2}, true},
		{"translation", []float64{1, 0, 40, 0, 1, -12, 0, 0, 1}, true},
		{"ill_conditioned", []float64{1e8, 0, 0, 0, 1e-8, 0, 0, 0, 1}, false},
		{"horizon", []float64{1, 0, 0, 0, 1, 0, -.02, 0, 1}, false},
		{"near_horizon", []float64{1, 0, 0, 0, 1, 0, (-1 + 5e-9) / 99, 0, 1}, false},
		{"last_pixel_center", []float64{1, 0, 0, 0, 1, 0, -1 / 99.5, 0, 1}, true},
		{"singular", []float64{1, 0, 0, 0, 0, 0, 0, 0, 1}, false},
		{"nan", []float64{math.NaN(), 0, 0, 0, 1, 0, 0, 0, 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			saved := append([]float64(nil), c.matrix...)
			err := VerifyMultiHomographySafety(context.Background(), c.matrix, 100, 100)
			if c.valid && err != nil {
				t.Fatal(err)
			}
			if !c.valid && !errors.Is(err, v2input.ErrInvalid) {
				t.Fatalf("unsafe matrix: %v", err)
			}
			for i, value := range saved {
				if !math.IsNaN(value) && c.matrix[i] != value {
					t.Fatal("input matrix mutated")
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyMultiHomographySafety(ctx, nil, 0, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel lost")
	}
	if err := VerifyMultiHomographySafety(context.Background(), cases[0].matrix, 0, 100); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("invalid dimensions")
	}
}

package v2optical

import (
	"context"
	"math"
	"photoeditor/backend/internal/v2input"
)

// VerifyMultiHomographySafety follows the multi worker's _matrix convention:
// normalize H33, bound the spectral condition to 1e8, and require a single
// denominator sign with margin 1e-8 at integer pixel centres [0,W-1]x[0,H-1].
// This proves numerical safety, not measured registration accuracy.
func VerifyMultiHomographySafety(ctx context.Context, values []float64, width, height int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(values) != 9 || math.IsNaN(values[8]) || math.IsInf(values[8], 0) || math.Abs(values[8]) <= 1e-10 {
		return v2input.ErrInvalid
	}
	var matrix [3][3]float64
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e8 {
			return v2input.ErrInvalid
		}
		matrix[i/3][i%3] = value / values[8]
	}
	return checkOpticalHomography(ctx, matrix, width, height, float64(width-1), float64(height-1), 1e-8)
}

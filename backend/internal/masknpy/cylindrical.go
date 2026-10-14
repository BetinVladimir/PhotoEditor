package masknpy

import (
	"context"
	"io"
	"math"
)

// VerifyCylindricalFieldNPY checks every half-pixel inverse coordinate against
// the worker projection, including exact float32 zeros outside source coverage.
// Cross-runtime math parity must be qualified before enabling this operation.
func VerifyCylindricalFieldNPY(ctx context.Context, reader io.Reader, size int64, sha string, width, height int64) error {
	focal := float64(max(width, height))
	cx, cy := float64(width)/2, float64(height)/2
	return verifyArrayNPY(ctx, reader, size, sha, width, height, 2, false, false, false, false, func(index int64, value float32) bool {
		pixel := index / 2
		x, y := pixel%width, pixel/width
		theta := (float64(x) + .5 - cx) / focal
		sx := focal*math.Tan(theta) + cx
		sy := (float64(y)+.5-cy)/math.Cos(theta) + cy
		expected := float32(0)
		if math.Abs(theta) < math.Pi/2 && sx >= .5 && sx <= float64(width)-.5 && sy >= .5 && sy <= float64(height)-.5 {
			if index%2 == 0 {
				expected = float32(sx)
			} else {
				expected = float32(sy)
			}
		}
		return math.Float32bits(value) == math.Float32bits(expected)
	}, false)
}

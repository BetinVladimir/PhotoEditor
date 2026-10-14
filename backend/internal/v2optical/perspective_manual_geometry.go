package v2optical

import (
	"context"
	"math"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveManualGeometry struct {
	PerspectiveManualBounds
	SourceToOutput [3][3]float64
}

// CompilePerspectiveManualGeometry solves the same eight pixel-edge equations
// as the worker, then checks determinant, spectral condition and horizon.
func CompilePerspectiveManualGeometry(ctx context.Context, params PerspectiveParams, width, height, maxPixels int64) (PerspectiveManualGeometry, error) {
	var zero PerspectiveManualGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	bounds, err := CompilePerspectiveManualBounds(params, width, height, maxPixels)
	if err != nil {
		return zero, err
	}
	identity := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	if params.Strength == 0 {
		return PerspectiveManualGeometry{PerspectiveManualBounds: bounds, SourceToOutput: identity}, nil
	}
	targets := [4][2]float64{{0, 0}, {float64(bounds.Width), 0}, {float64(bounds.Width), float64(bounds.Height)}, {0, float64(bounds.Height)}}
	var system [8][9]float64
	for i, p := range bounds.Corners {
		x, y, u, v := p.X, p.Y, targets[i][0], targets[i][1]
		system[2*i] = [9]float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		system[2*i+1] = [9]float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	// Partial pivoting; no tolerance-based geometry fallback or corner reordering.
	for col := 0; col < 8; col++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		pivot := col
		for row := col + 1; row < 8; row++ {
			if math.Abs(system[row][col]) > math.Abs(system[pivot][col]) {
				pivot = row
			}
		}
		if system[pivot][col] == 0 || math.IsNaN(system[pivot][col]) || math.IsInf(system[pivot][col], 0) {
			return zero, v2input.ErrInvalid
		}
		system[col], system[pivot] = system[pivot], system[col]
		for row := col + 1; row < 8; row++ {
			factor := system[row][col] / system[col][col]
			system[row][col] = 0
			for j := col + 1; j < 9; j++ {
				system[row][j] -= factor * system[col][j]
			}
		}
	}
	var solved [8]float64
	for row := 7; row >= 0; row-- {
		sum := system[row][8]
		for col := row + 1; col < 8; col++ {
			sum -= system[row][col] * solved[col]
		}
		solved[row] = sum / system[row][row]
	}
	matrix := [3][3]float64{{solved[0], solved[1], solved[2]}, {solved[3], solved[4], solved[5]}, {solved[6], solved[7], 1}}
	// Match the worker manual solver's authored quad horizon. Full source horizon
	// is checked later by its warp/coverage guard and must also hold here.
	if err := checkPerspectiveHomography(ctx, matrix, width, height); err != nil {
		return zero, err
	}
	return PerspectiveManualGeometry{PerspectiveManualBounds: bounds, SourceToOutput: matrix}, nil
}

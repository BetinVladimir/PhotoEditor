package v2optical

import (
	"context"
	"math"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveAutoProposal is intermediate edge-coordinate geometry. It is not
// admissible until effective-strength coverage and input authority checks pass.
type PerspectiveAutoProposal struct {
	SourceToOutput [3][3]float64
	Width, Height  int64
}

func perspectiveRectifiedDirection(ctx context.Context, lines []PerspectiveLine, family string, horizon [3]float64, width, height int64) ([2]float64, error) {
	var sum [2]float64
	count := 0
	axis := 0
	if family == "vertical" {
		axis = 1
	}
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return [2]float64{}, err
		}
		if line.Family != family {
			continue
		}
		var mapped [2][2]float64
		var previous float64
		for i, p := range [2]PerspectivePoint{line.A, line.B} {
			x, y := p.X*float64(width), p.Y*float64(height)
			den := x*horizon[0] + y*horizon[1] + 1
			if math.Abs(den) <= 1e-10 || (i == 1 && previous*den <= 0) {
				return [2]float64{}, v2input.ErrInvalid
			}
			mapped[i] = [2]float64{x / den, y / den}
			previous = den
		}
		direction := [2]float64{mapped[1][0] - mapped[0][0], mapped[1][1] - mapped[0][1]}
		length := math.Hypot(direction[0], direction[1])
		if length <= 1e-10 {
			return [2]float64{}, v2input.ErrInvalid
		}
		sign := 1.
		if direction[axis] < 0 {
			sign = -1
		}
		sum[0] += sign * direction[0] / length
		sum[1] += sign * direction[1] / length
		count++
	}
	if count < 2 {
		return [2]float64{}, v2input.ErrInvalid
	}
	sum[0] /= float64(count)
	sum[1] /= float64(count)
	length := math.Hypot(sum[0], sum[1])
	if length <= 1e-10 || math.IsNaN(length) || math.IsInf(length, 0) {
		return [2]float64{}, v2input.ErrInvalid
	}
	return [2]float64{sum[0] / length, sum[1] / length}, nil
}

// CompilePerspectiveAutoProposal reconstructs projective plus affine family
// rectification. Original LineSegments validation is repeated by the seed solver.
func CompilePerspectiveAutoProposal(ctx context.Context, lines PerspectiveLineSegments, width, height, maxPixels int64) (PerspectiveAutoProposal, error) {
	var zero PerspectiveAutoProposal
	if maxPixels < 1 || maxPixels > 200000000 {
		return zero, v2input.ErrInvalid
	}
	seed, err := CompilePerspectiveVanishingGeometry(ctx, lines, width, height)
	if err != nil {
		return zero, err
	}
	projective := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {seed.Horizon[0], seed.Horizon[1], 1}}
	if err := checkPerspectiveHomography(ctx, projective, width, height); err != nil {
		return zero, err
	}
	h, err := perspectiveRectifiedDirection(ctx, lines.Lines, "horizontal", seed.Horizon, width, height)
	if err != nil {
		return zero, err
	}
	v, err := perspectiveRectifiedDirection(ctx, lines.Lines, "vertical", seed.Horizon, width, height)
	if err != nil {
		return zero, err
	}
	determinant := h[0]*v[1] - v[0]*h[1]
	// Columns are unit vectors; sigma_max^2=1+abs(dot). sigma_min is
	// abs(det)/sigma_max, avoiding cancellation in 1-abs(dot).
	dot := h[0]*v[0] + h[1]*v[1]
	condition := (1 + math.Abs(dot)) / math.Abs(determinant)
	if math.Abs(determinant) <= 1e-10 || condition > 1e8 || math.IsNaN(condition) {
		return zero, v2input.ErrInvalid
	}
	matrix := [3][3]float64{{v[1] / determinant, -v[0] / determinant, 0}, {-h[1] / determinant, h[0] / determinant, 0}, {seed.Horizon[0], seed.Horizon[1], 1}}
	return perspectiveTranslatedBounds(ctx, matrix, width, height, maxPixels)
}

func perspectiveTranslatedBounds(ctx context.Context, matrix [3][3]float64, width, height, maxPixels int64) (PerspectiveAutoProposal, error) {
	var zero PerspectiveAutoProposal
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if width < 1 || height < 1 || maxPixels < 1 || maxPixels > 200000000 || width > 200000000 || height > 200000000/width {
		return zero, v2input.ErrInvalid
	}
	for _, row := range matrix {
		for _, value := range row {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return zero, v2input.ErrInvalid
			}
		}
	}
	if err := checkPerspectiveHomography(ctx, matrix, width, height); err != nil {
		return zero, err
	}
	minimum := [2]float64{math.Inf(1), math.Inf(1)}
	maximum := [2]float64{math.Inf(-1), math.Inf(-1)}
	sign := 0
	for _, p := range [][2]float64{{0, 0}, {float64(width), 0}, {float64(width), float64(height)}, {0, float64(height)}} {
		den := matrix[2][0]*p[0] + matrix[2][1]*p[1] + matrix[2][2]
		if math.Abs(den) <= 1e-10 {
			return zero, v2input.ErrInvalid
		}
		current := 1
		if den < 0 {
			current = -1
		}
		if sign != 0 && sign != current {
			return zero, v2input.ErrInvalid
		}
		sign = current
		for axis := 0; axis < 2; axis++ {
			value := (matrix[axis][0]*p[0] + matrix[axis][1]*p[1] + matrix[axis][2]) / den
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return zero, v2input.ErrInvalid
			}
			nearest := math.RoundToEven(value)
			tolerance := 64 * (math.Nextafter(1, 2) - 1) * math.Max(1, math.Abs(value))
			if math.Abs(value-nearest) <= tolerance {
				value = nearest
			}
			minimum[axis] = math.Min(minimum[axis], math.Floor(value))
			maximum[axis] = math.Max(maximum[axis], math.Ceil(value))
		}
	}
	w, h := maximum[0]-minimum[0], maximum[1]-minimum[1]
	if w < 1 || h < 1 || w > float64(maxPixels) || h > float64(maxPixels) || w*h > float64(maxPixels) {
		return zero, v2input.ErrInvalid
	}
	for axis := 0; axis < 2; axis++ {
		for col := 0; col < 3; col++ {
			matrix[axis][col] -= minimum[axis] * matrix[2][col]
		}
	}
	if err := checkPerspectiveHomography(ctx, matrix, width, height); err != nil {
		return zero, err
	}
	return PerspectiveAutoProposal{SourceToOutput: matrix, Width: int64(w), Height: int64(h)}, nil
}

// PerspectiveAutoCoverage counts supported output centers with constant memory.
// Coverage describes geometric support, independently of source alpha. Callers
// must still validate input authority before admitting a task.
func PerspectiveAutoCoverage(ctx context.Context, proposal PerspectiveAutoProposal, width, height int64) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width || proposal.Width < 1 || proposal.Height < 1 || proposal.Width > 200000000 || proposal.Height > 200000000/proposal.Width {
		return 0, v2input.ErrInvalid
	}
	m := proposal.SourceToOutput
	if err := checkPerspectiveHomography(ctx, m, width, height); err != nil {
		return 0, err
	}
	for _, row := range m {
		for _, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, v2input.ErrInvalid
			}
		}
	}
	inverse := [3][3]float64{
		{m[1][1]*m[2][2] - m[1][2]*m[2][1], m[0][2]*m[2][1] - m[0][1]*m[2][2], m[0][1]*m[1][2] - m[0][2]*m[1][1]},
		{m[1][2]*m[2][0] - m[1][0]*m[2][2], m[0][0]*m[2][2] - m[0][2]*m[2][0], m[0][2]*m[1][0] - m[0][0]*m[1][2]},
		{m[1][0]*m[2][1] - m[1][1]*m[2][0], m[0][1]*m[2][0] - m[0][0]*m[2][1], m[0][0]*m[1][1] - m[0][1]*m[1][0]}}
	determinant := m[0][0]*inverse[0][0] + m[0][1]*inverse[1][0] + m[0][2]*inverse[2][0]
	if math.Abs(determinant) <= 1e-10 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return 0, v2input.ErrInvalid
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			inverse[i][j] /= determinant
		}
	}
	covered := int64(0)
	for y := int64(0); y < proposal.Height; y++ {
		for x := int64(0); x < proposal.Width; x++ {
			if x%512 == 0 {
				if err := ctx.Err(); err != nil {
					return 0, err
				}
			}
			xx, yy := float64(x)+.5, float64(y)+.5
			den := inverse[2][0]*xx + inverse[2][1]*yy + inverse[2][2]
			if math.Abs(den) <= 1e-10 {
				continue
			}
			sx := (inverse[0][0]*xx+inverse[0][1]*yy+inverse[0][2])/den - .5
			sy := (inverse[1][0]*xx+inverse[1][1]*yy+inverse[1][2])/den - .5
			if sx >= 0 && sx <= float64(width-1) && sy >= 0 && sy <= float64(height-1) {
				covered++
			}
		}
	}
	return 1 - float64(covered)/float64(proposal.Width*proposal.Height), nil
}

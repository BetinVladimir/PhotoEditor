package v2optical

import (
	"context"
	"math"
	"photoeditor/backend/internal/v2input"
)

// checkPerspectiveHomography enforces the worker float64 determinant, spectral
// condition and horizon rules. One-sided rotations avoid normal equations.
func checkPerspectiveHomography(ctx context.Context, m [3][3]float64, width, height int64) error {
	return checkOpticalHomography(ctx, m, width, height, float64(width), float64(height), 1e-10)
}

// Shared spectral safety calculation; callers select their own pixel-centre
// horizon domain and epsilon. Existing perspective conventions stay unchanged.
func checkOpticalHomography(ctx context.Context, m [3][3]float64, width, height int64, maxX, maxY, horizonEpsilon float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return v2input.ErrInvalid
	}
	scale := 0.
	for _, row := range m {
		for _, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return v2input.ErrInvalid
			}
			scale = math.Max(scale, math.Abs(v))
		}
	}
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) - m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) + m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if math.Abs(det) <= 1e-10 || math.IsNaN(det) || math.IsInf(det, 0) || scale == 0 {
		return v2input.ErrInvalid
	}
	rows := m
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			rows[i][j] /= scale
		}
	}
	converged := false
	for sweep := 0; sweep < 64; sweep++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed := false
		for _, pair := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
			p, q := pair[0], pair[1]
			aa, bb, ab := 0., 0., 0.
			for _, r := range rows {
				aa += r[p] * r[p]
				bb += r[q] * r[q]
				ab += r[p] * r[q]
			}
			if aa == 0 || bb == 0 || math.Abs(ab) <= 8*(math.Nextafter(1, 2)-1)*math.Sqrt(aa)*math.Sqrt(bb) {
				continue
			}
			tau := (bb - aa) / (2 * ab)
			t := math.Copysign(1, tau) / (math.Abs(tau) + math.Hypot(1, tau))
			c := 1 / math.Hypot(1, t)
			s := c * t
			for i := 0; i < 3; i++ {
				x, y := rows[i][p], rows[i][q]
				rows[i][p] = c*x - s*y
				rows[i][q] = s*x + c*y
			}
			changed = true
		}
		if !changed {
			converged = true
			break
		}
	}
	if !converged {
		return v2input.ErrInvalid
	}
	smallest, largest := math.Inf(1), 0.
	for j := 0; j < 3; j++ {
		norm := math.Hypot(math.Hypot(rows[0][j], rows[1][j]), rows[2][j])
		smallest = math.Min(smallest, norm)
		largest = math.Max(largest, norm)
	}
	if smallest == 0 || largest/smallest > 1e8 {
		return v2input.ErrInvalid
	}
	sign := 0
	for _, p := range [][2]float64{{0, 0}, {maxX, 0}, {maxX, maxY}, {0, maxY}} {
		den := m[2][0]*p[0] + m[2][1]*p[1] + m[2][2]
		if math.Abs(den) <= horizonEpsilon || math.IsNaN(den) || math.IsInf(den, 0) {
			return v2input.ErrInvalid
		}
		current := 1
		if den < 0 {
			current = -1
		}
		if sign != 0 && sign != current {
			return v2input.ErrInvalid
		}
		sign = current
	}
	return nil
}

// PerspectiveAutoGeometry binds effective strength to one output and crop value.
// It has no authority to publish, qualify or admit the line document.
type PerspectiveAutoGeometry struct {
	PerspectiveAutoProposal
	CropFraction float64
}

func CompilePerspectiveAutoGeometry(ctx context.Context, params PerspectiveParams, lines PerspectiveLineSegments, width, height, maxPixels int64) (PerspectiveAutoGeometry, error) {
	var zero PerspectiveAutoGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if params.Mode != "auto" {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := lines.Compile(); err != nil {
		return zero, err
	}
	identity := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	if params.Strength == 0 {
		bounds, err := CompilePerspectiveBounds(params, width, height, maxPixels)
		if err != nil {
			return zero, err
		}
		return PerspectiveAutoGeometry{PerspectiveAutoProposal: PerspectiveAutoProposal{SourceToOutput: identity, Width: bounds.Width, Height: bounds.Height}}, nil
	}
	proposal, err := CompilePerspectiveAutoProposal(ctx, lines, width, height, maxPixels)
	if err != nil {
		return zero, err
	}
	if params.Strength != 1 {
		m := proposal.SourceToOutput
		if math.Abs(m[2][2]) <= 1e-10 {
			return zero, v2input.ErrInvalid
		}
		z := m[2][2]
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				m[i][j] = identity[i][j] + params.Strength*(m[i][j]/z-identity[i][j])
			}
		}
		proposal, err = perspectiveTranslatedBounds(ctx, m, width, height, maxPixels)
		if err != nil {
			return zero, err
		}
	}
	crop, err := PerspectiveAutoCoverage(ctx, proposal, width, height)
	if err != nil {
		return zero, err
	}
	if crop > params.MaxCropFraction {
		return zero, v2input.ErrInvalid
	}
	return PerspectiveAutoGeometry{PerspectiveAutoProposal: proposal, CropFraction: crop}, nil
}

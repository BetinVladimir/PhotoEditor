package v2optical

import (
	"context"
	"math"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// perspectiveVanishingPoint uses one-sided Jacobi SVD of normalized line rows.
// It avoids squaring the condition number through an A-transpose-A eigensolve.
// The homogeneous result also represents parallel lines (a point at infinity).
func perspectiveVanishingPoint(ctx context.Context, segments []PerspectiveLine, width, height int64) ([3]float64, error) {
	var zero [3]float64
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(segments) < 2 || len(segments) > 1000 || width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return zero, v2input.ErrInvalid
	}
	rows := make([][3]float64, len(segments))
	for i, line := range segments {
		for _, p := range []PerspectivePoint{line.A, line.B} {
			if !negativeRange(p.X, 0, 1) || !negativeRange(p.Y, 0, 1) {
				return zero, v2input.ErrInvalid
			}
		}
		ax, ay := line.A.X*float64(width), line.A.Y*float64(height)
		bx, by := line.B.X*float64(width), line.B.Y*float64(height)
		length := math.Hypot(ay-by, bx-ax)
		if length <= 1e-10 {
			return zero, v2input.ErrInvalid
		}
		rows[i] = [3]float64{(ay - by) / length, (bx - ax) / length, (ax*by - ay*bx) / length}
	}
	right := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	converged := false
	for sweep := 0; sweep < 64; sweep++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		changed := false
		for _, pair := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
			p, q := pair[0], pair[1]
			var aa, bb, ab float64
			for _, r := range rows {
				aa += r[p] * r[p]
				bb += r[q] * r[q]
				ab += r[p] * r[q]
			}
			if aa == 0 || bb == 0 || math.Abs(ab) <= 8*(math.Nextafter(1, 2)-1)*math.Sqrt(aa)*math.Sqrt(bb) {
				continue
			}
			tau := (bb - aa) / (2 * ab)
			tangent := math.Copysign(1, tau) / (math.Abs(tau) + math.Hypot(1, tau))
			cosine := 1 / math.Hypot(1, tangent)
			sine := cosine * tangent
			for i := range rows {
				x, y := rows[i][p], rows[i][q]
				rows[i][p] = cosine*x - sine*y
				rows[i][q] = sine*x + cosine*y
			}
			for i := 0; i < 3; i++ {
				x, y := right[i][p], right[i][q]
				right[i][p] = cosine*x - sine*y
				right[i][q] = sine*x + cosine*y
			}
			changed = true
		}
		if !changed {
			converged = true
			break
		}
	}
	if !converged {
		return zero, v2input.ErrInvalid
	}
	var singular [3]float64
	for _, r := range rows {
		for i := 0; i < 3; i++ {
			singular[i] = math.Hypot(singular[i], r[i])
		}
	}
	indices := []int{0, 1, 2}
	sort.Slice(indices, func(i, j int) bool { return singular[indices[i]] > singular[indices[j]] })
	middle, small := singular[indices[1]], singular[indices[2]]
	if middle <= 1e-10 || (small > 0 && middle/small < 10) {
		return zero, v2input.ErrInvalid
	}
	result := [3]float64{right[0][indices[2]], right[1][indices[2]], right[2][indices[2]]}
	for _, v := range result {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return zero, v2input.ErrInvalid
		}
	}
	return result, nil
}

// PerspectiveVanishingGeometry is the normalized projective rectification seed.
// Affine family rectification, output bounds and coverage must be resolved before
// this seed can be used for admission; it is deliberately not a task descriptor.
type PerspectiveVanishingGeometry struct {
	Horizontal [3]float64
	Vertical   [3]float64
	Horizon    [3]float64
}

func CompilePerspectiveVanishingGeometry(ctx context.Context, lines PerspectiveLineSegments, width, height int64) (PerspectiveVanishingGeometry, error) {
	var zero PerspectiveVanishingGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, _, err := lines.Compile(); err != nil {
		return zero, err
	}
	horizontal := make([]PerspectiveLine, 0, len(lines.Lines))
	vertical := make([]PerspectiveLine, 0, len(lines.Lines))
	for _, line := range lines.Lines {
		if line.Family == "horizontal" {
			horizontal = append(horizontal, line)
		} else {
			vertical = append(vertical, line)
		}
	}
	h, err := perspectiveVanishingPoint(ctx, horizontal, width, height)
	if err != nil {
		return zero, err
	}
	v, err := perspectiveVanishingPoint(ctx, vertical, width, height)
	if err != nil {
		return zero, err
	}
	horizon := [3]float64{h[1]*v[2] - h[2]*v[1], h[2]*v[0] - h[0]*v[2], h[0]*v[1] - h[1]*v[0]}
	if math.Abs(horizon[2]) <= 1e-8 {
		return zero, v2input.ErrInvalid
	}
	z := horizon[2]
	for i := range horizon {
		horizon[i] /= z
	}
	// Every source corner must remain on one side of the projective horizon.
	sign := 0
	for _, p := range [][2]float64{{0, 0}, {float64(width), 0}, {float64(width), float64(height)}, {0, float64(height)}} {
		den := horizon[0]*p[0] + horizon[1]*p[1] + 1
		if math.IsNaN(den) || math.IsInf(den, 0) || math.Abs(den) <= 1e-10 {
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
	}
	return PerspectiveVanishingGeometry{Horizontal: h, Vertical: v, Horizon: horizon}, nil
}

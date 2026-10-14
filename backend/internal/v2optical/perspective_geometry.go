package v2optical

import (
	"math"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveManualBounds owns the effective pixel-edge quad and output size.
// Homography stability and physical coverage remain separate solver checks.
type PerspectiveManualBounds struct {
	Corners      [4]PerspectivePoint
	Width        int64
	Height       int64
	CropFraction float64
}

func CompilePerspectiveManualBounds(params PerspectiveParams, width, height, maxPixels int64) (PerspectiveManualBounds, error) {
	var zero PerspectiveManualBounds
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if params.Mode != "manual" || width < 1 || height < 1 || width > 200000000 || height > 200000000/width || maxPixels < 1 || maxPixels > 200000000 {
		return zero, v2input.ErrInvalid
	}
	full := [4]PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	var points [4]PerspectivePoint
	for i, p := range params.Corners {
		points[i] = PerspectivePoint{(full[i].X + params.Strength*(p.X-full[i].X)) * float64(width),
			(full[i].Y + params.Strength*(p.Y-full[i].Y)) * float64(height)}
	}
	var area float64
	var edges [4]float64
	for i, p := range points {
		next := points[(i+1)%4]
		area += p.X*next.Y - p.Y*next.X
		edges[i] = math.Hypot(next.X-p.X, next.Y-p.Y)
		after := points[(i+2)%4]
		if (next.X-p.X)*(after.Y-next.Y)-(next.Y-p.Y)*(after.X-next.X) <= 0 {
			return zero, v2input.ErrInvalid
		}
	}
	crop := math.Max(0, 1-(area*.5)/float64(width*height))
	if crop > params.MaxCropFraction {
		return zero, v2input.ErrInvalid
	}
	// Python round is ties-to-even; keep plan pixel_count identical to worker.
	outWidth := int64(math.Max(1, math.RoundToEven((edges[0]+edges[2])/2)))
	outHeight := int64(math.Max(1, math.RoundToEven((edges[1]+edges[3])/2)))
	if outWidth > maxPixels || outHeight > maxPixels/outWidth {
		return zero, v2input.ErrInvalid
	}
	return PerspectiveManualBounds{Corners: points, Width: outWidth, Height: outHeight, CropFraction: crop}, nil
}

// CompilePerspectiveBounds resolves the identity path before choosing a solver.
// Auto identity still requires valid Params and its normal qualified input closure.
func CompilePerspectiveBounds(params PerspectiveParams, width, height, maxPixels int64) (PerspectiveManualBounds, error) {
	if _, _, err := params.Compile(); err != nil {
		return PerspectiveManualBounds{}, err
	}
	if params.Strength != 0 {
		return CompilePerspectiveManualBounds(params, width, height, maxPixels)
	}
	if width < 1 || height < 1 || maxPixels < 1 || maxPixels > 200000000 || width > maxPixels || height > maxPixels/width {
		return PerspectiveManualBounds{}, v2input.ErrInvalid
	}
	return PerspectiveManualBounds{Corners: [4]PerspectivePoint{{0, 0}, {float64(width), 0}, {float64(width), float64(height)}, {0, float64(height)}}, Width: width, Height: height}, nil
}

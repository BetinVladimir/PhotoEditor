package v2region

import (
	"errors"
	"math"
)

var ErrRegionInvalid = errors.New("REGION_PREVIEW_INVALID")

type RegionRect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// RegionGeometry uses the already-oriented canonical canvas. Callers must
// separately authorize immutable source/recipe before applying this calculation.
type RegionGeometry struct {
	Left         int64 `json:"left"`
	Top          int64 `json:"top"`
	Width        int64 `json:"width"`
	Height       int64 `json:"height"`
	OutputWidth  int64 `json:"output_width"`
	OutputHeight int64 `json:"output_height"`
}

func CompileRegionGeometry(width, height int64, rect RegionRect, maxEdge int) (RegionGeometry, error) {
	var empty RegionGeometry
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width || maxEdge < 64 || maxEdge > 1024 {
		return empty, ErrRegionInvalid
	}
	for _, v := range []float64{rect.X0, rect.Y0, rect.X1, rect.Y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return empty, ErrRegionInvalid
		}
	}
	if rect.X0 >= rect.X1 || rect.Y0 >= rect.Y1 {
		return empty, ErrRegionInvalid
	}
	left, top := int64(math.Floor(rect.X0*float64(width))), int64(math.Floor(rect.Y0*float64(height)))
	right, bottom := int64(math.Ceil(rect.X1*float64(width))), int64(math.Ceil(rect.Y1*float64(height)))
	w, h := right-left, bottom-top
	if w < 1 || h < 1 {
		return empty, ErrRegionInvalid
	}
	scale := math.Min(1, float64(maxEdge)/float64(max(w, h)))
	return RegionGeometry{left, top, w, h, max(int64(1), int64(math.Floor(float64(w)*scale))), max(int64(1), int64(math.Floor(float64(h)*scale)))}, nil
}

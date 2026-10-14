package review

import "errors"

// ManualRasterWork is a conservative integer work bound, not a wall-time or
// model qualification estimate. It ignores spatial skips to avoid undercharging.
type ManualRasterWork struct {
	Pixels                int64
	RowSegments           int64
	ShapeBoundPointChecks int64
	SampleEdgeEvaluations int64
	NPYPayloadBytes       int64
	ExpectedSegmentBytes  int64
}

// EstimateStreamWork describes one complete paired NPY verification pass using
// 1024-sample row segments. Repeated passes must be charged separately.
func (p ManualRasterPlan) EstimateStreamWork() (ManualRasterWork, error) {
	if len(p.body) == 0 || !validCanvas(p.canvas) || p.canvas.Width*p.canvas.Height > 200000000 {
		return ManualRasterWork{}, errors.New("invalid raster work plan")
	}
	pixels := p.canvas.Width * p.canvas.Height
	segments := ((p.canvas.Width + 1023) / 1024) * p.canvas.Height
	var points, edges int64
	for _, shape := range p.vectors.Shapes {
		points += int64(len(shape.Points))
		if shape.Kind == "stroke" {
			edges += max(int64(1), int64(len(shape.Points)-1))
		} else {
			edges += 4 * int64(len(shape.Points))
		}
	}
	return ManualRasterWork{Pixels: pixels, RowSegments: segments, ShapeBoundPointChecks: segments * points, SampleEdgeEvaluations: pixels * edges, NPYPayloadBytes: 8 * pixels, ExpectedSegmentBytes: 8 * min(p.canvas.Width, int64(1024))}, nil
}

// CheckStreamBudget checks caller-selected qualified limits before object IO. Zero
// limits never mean unlimited. Stream headers/catalog/JSON are separate charges.
func (p ManualRasterPlan) CheckStreamBudget(maxPointChecks, maxEdgeEvaluations, maxPayloadBytes int64) (ManualRasterWork, error) {
	work, err := p.EstimateStreamWork()
	if err != nil {
		return ManualRasterWork{}, err
	}
	if err = work.checkBudget(maxPointChecks, maxEdgeEvaluations, maxPayloadBytes); err != nil {
		return ManualRasterWork{}, err
	}
	return work, nil
}

func (w ManualRasterWork) checkBudget(maxPointChecks, maxEdgeEvaluations, maxPayloadBytes int64) error {
	if w.Pixels < 1 || maxPointChecks < 0 || maxEdgeEvaluations < 0 || maxPayloadBytes < 0 || w.ShapeBoundPointChecks > maxPointChecks || w.SampleEdgeEvaluations > maxEdgeEvaluations || w.NPYPayloadBytes > maxPayloadBytes {
		return errors.New("manual raster budget exceeded")
	}
	return nil
}

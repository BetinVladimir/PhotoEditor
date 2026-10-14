package review

import (
	"context"
	"errors"
	"math"
)

// RasterizeRegion independently reconstructs original vector probabilities in
// a bounded rectangle using the worker's pixel-center/float32 composition rules.
// Returned owned arrays are row-major; this does not verify stored NPY artifacts.
func (p ManualRasterPlan) RasterizeRegion(ctx context.Context, x, y, width, height int64) ([]float32, []float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(p.body) == 0 || x < 0 || y < 0 || width < 1 || height < 1 || width > 1024 || height > 1024 || x > p.canvas.Width-width || y > p.canvas.Height-height {
		return nil, nil, errors.New("invalid raster region")
	}
	target := make([]float32, width*height)
	protected := make([]float32, len(target))
	w, h := float64(p.canvas.Width), float64(p.canvas.Height)
	for _, shape := range p.vectors.Shapes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for i, point := range shape.Points {
			if i%64 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
			}
			minX = math.Min(minX, point.X*w)
			maxX = math.Max(maxX, point.X*w)
			minY = math.Min(minY, point.Y*h)
			maxY = math.Max(maxY, point.Y*h)
		}
		radius := float64(0)
		if shape.Kind == "stroke" {
			radius = *shape.Radius * math.Max(w, h)
		}
		// Match the worker's conservative one-pixel-expanded tile bounds.
		if maxX+radius+1 < float64(x) || maxY+radius+1 < float64(y) || minX-radius-1 > float64(x+width) || minY-radius-1 > float64(y+height) {
			continue
		}
		for row := int64(0); row < height; row++ {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			for col := int64(0); col < width; col++ {
				if col%256 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, nil, err
					}
				}
				px, py := float64(x+col)+.5, float64(y+row)+.5
				var coverage float32
				if shape.Kind == "stroke" {
					distance := math.Inf(1)
					segments := len(shape.Points) - 1
					if segments == 0 {
						segments = 1
					}
					for i := 0; i < segments; i++ {
						if i%64 == 0 {
							if err := ctx.Err(); err != nil {
								return nil, nil, err
							}
						}
						a := shape.Points[i]
						b := a
						if len(shape.Points) > 1 {
							b = shape.Points[i+1]
						}
						ax, ay := a.X*w, a.Y*h
						dx, dy := b.X*w-ax, b.Y*h-ay
						denominator := dx*dx + dy*dy
						t := float64(0)
						if denominator != 0 {
							t = clampProbability(((px-ax)*dx + (py-ay)*dy) / denominator)
						}
						distance = math.Min(distance, math.Hypot(px-(ax+t*dx), py-(ay+t*dy)))
					}
					radius := *shape.Radius * math.Max(w, h)
					if *shape.Hardness == 1 {
						coverage = float32(clampProbability(radius + .5 - distance))
					} else {
						inner := radius * *shape.Hardness
						t := clampProbability((radius - distance) / math.Max(radius-inner, 2.220446049250313e-16))
						coverage = float32(t * t * (3 - 2*t))
					}
				} else {
					for _, offset := range [4][2]float64{{-.25, -.25}, {.25, -.25}, {-.25, .25}, {.25, .25}} {
						inside := false
						qx, qy := px+offset[0], py+offset[1]
						for i, a := range shape.Points {
							if i%64 == 0 {
								if err := ctx.Err(); err != nil {
									return nil, nil, err
								}
							}
							b := shape.Points[(i+1)%len(shape.Points)]
							ax, ay, bx, by := a.X*w, a.Y*h, b.X*w, b.Y*h
							if ay == by {
								continue
							}
							if (ay > qy) != (by > qy) && qx < (bx-ax)*(qy-ay)/(by-ay)+ax {
								inside = !inside
							}
						}
						if inside {
							coverage += .25
						}
					}
				}
				index := row*width + col
				switch shape.Mode {
				case "add":
					if coverage > target[index] {
						target[index] = coverage
					}
				case "subtract":
					factor := float32(1) - coverage
					target[index] = target[index] * factor
				case "protect":
					if coverage > protected[index] {
						protected[index] = coverage
					}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return target, protected, nil
}
func clampProbability(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

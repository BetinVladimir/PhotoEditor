package interactive

import (
	"encoding/json"
	"photoeditor/backend/internal/domain"
	"regexp"
	"strings"
)

var promptSelector = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$`)

// Empty arrays do not choose a producer. Parse every supplied field before
// creating graph ownership; malformed unused inputs are never silently ignored.
func parseMaskInputs(req MaskCreate) (shapes, points []any, manual bool, err error) {
	strokes, err := array(req.Strokes, 512<<10)
	if err != nil {
		return nil, nil, false, err
	}
	polygons, err := array(req.Polygons, 512<<10)
	if err != nil {
		return nil, nil, false, err
	}
	points, err = array(req.Points, 128<<10)
	if err != nil {
		return nil, nil, false, err
	}
	shapes = append(strokes, polygons...)
	manual = len(shapes) > 0
	if manual == (len(points) > 0) || len(shapes) > 1024 || len(points) > 128 {
		return nil, nil, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	if manual {
		if err := validateManualShapes(shapes); err != nil {
			return nil, nil, false, err
		}
	}
	positive := false
	for _, item := range points {
		point, ok := item.(map[string]any)
		if !ok || len(point) != 3 {
			return nil, nil, false, domain.NewError("VALIDATION_FAILED", 422)
		}
		x, xok := point["x"].(float64)
		y, yok := point["y"].(float64)
		label, lok := point["label"].(string)
		if !xok || !yok || !lok || x < 0 || x > 1 || y < 0 || y > 1 || (label != "positive" && label != "negative") {
			return nil, nil, false, domain.NewError("VALIDATION_FAILED", 422)
		}
		positive = positive || label == "positive"
	}
	if !manual {
		if !positive {
			return nil, nil, false, domain.NewError("VALIDATION_FAILED", 422)
		}
		if _, _, err := parsePromptOptions(req); err != nil {
			return nil, nil, false, err
		}
	}
	return shapes, points, manual, nil
}

// Match PromptMaskParams before graph ownership is created.
func parsePromptOptions(req MaskCreate) (string, any, error) {
	selector := strings.TrimSpace(req.Selector)
	if selector == "" {
		selector = "manual"
	}
	invalid := func() (string, any, error) { return "", nil, domain.NewError("VALIDATION_FAILED", 422) }
	if !promptSelector.MatchString(selector) {
		return invalid()
	}
	if len(req.Box) == 0 {
		return selector, nil, nil
	}
	if len(req.Box) > 4096 {
		return invalid()
	}
	var box any
	if json.Unmarshal(req.Box, &box) != nil {
		return invalid()
	}
	if box == nil {
		return selector, nil, nil
	}
	rect, ok := box.(map[string]any)
	if !ok || len(rect) != 4 {
		return invalid()
	}
	coords := make([]float64, 4)
	for i, key := range []string{"x0", "y0", "x1", "y1"} {
		value, ok := rect[key].(float64)
		if !ok || value < 0 || value > 1 {
			return invalid()
		}
		coords[i] = value
	}
	if coords[2] <= coords[0] || coords[3] <= coords[1] {
		return invalid()
	}
	return selector, rect, nil
}

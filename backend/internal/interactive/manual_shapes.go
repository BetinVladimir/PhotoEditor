package interactive

import "photoeditor/backend/internal/domain"

// Validate client geometry before committing an unmaterialized producer. Canvas
// binding and pixel/work limits remain the scheduler's independent obligation.
func validateManualShapes(shapes []any) error {
	invalid := func() error { return domain.NewError("VALIDATION_FAILED", 422) }
	seen := map[float64]bool{}
	total := 0
	unit := func(value any) (float64, bool) {
		n, ok := value.(float64)
		return n, ok && n >= 0 && n <= 1
	}
	for _, item := range shapes {
		shape, ok := item.(map[string]any)
		if !ok {
			return invalid()
		}
		kind, kok := shape["kind"].(string)
		mode, mok := shape["mode"].(string)
		sequence, sok := shape["sequence"].(float64)
		if !kok || !mok || !sok || sequence < 0 || sequence > 2147483647 || sequence != float64(int64(sequence)) || seen[sequence] || (mode != "add" && mode != "subtract" && mode != "protect") {
			return invalid()
		}
		seen[sequence] = true
		points, ok := shape["points"].([]any)
		if !ok || len(points) > 4096 {
			return invalid()
		}
		switch kind {
		case "stroke":
			radius, rok := unit(shape["radius"])
			_, hok := unit(shape["hardness"])
			if len(shape) != 6 || len(points) < 1 || !rok || radius == 0 || !hok {
				return invalid()
			}
		case "polygon":
			if len(shape) != 4 || len(points) < 3 {
				return invalid()
			}
		default:
			return invalid()
		}
		total += len(points)
		if total > 10000 {
			return invalid()
		}
		for _, item := range points {
			point, ok := item.(map[string]any)
			if !ok || len(point) != 2 {
				return invalid()
			}
			if _, ok := unit(point["x"]); !ok {
				return invalid()
			}
			if _, ok := unit(point["y"]); !ok {
				return invalid()
			}
		}
	}
	return nil
}

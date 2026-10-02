package v2input

import (
	"math"

	"github.com/google/uuid"
)

// CaptureConfirmedFrame is a previously confirmed frame from the frozen session
// snapshot. Callers must authorize its person IDs and exclude the current receipt.
// A nil Quality means that no qualified quality assessment exists.
type CaptureConfirmedFrame struct {
	AssetVersionID uuid.UUID
	PersonIDs      []uuid.UUID
	Quality        *float64
}

// CompileCaptureCoverage counts each version once per requirement. Automatic
// matches must never be supplied as confirmed people. Conflicting duplicate
// versions fail rather than selecting evidence by input order.
func CompileCaptureCoverage(shots CaptureShotList, frames []CaptureConfirmedFrame) ([]CaptureCoverage, error) {
	if _, err := validateCaptureShots(shots); err != nil {
		return nil, err
	}
	if frames == nil || len(frames) > 5000 {
		return nil, ErrInvalid
	}
	unique := make(map[uuid.UUID]CaptureConfirmedFrame, len(frames))
	people := make(map[uuid.UUID]map[uuid.UUID]bool, len(frames))
	for _, frame := range frames {
		if frame.AssetVersionID == uuid.Nil || frame.PersonIDs == nil || len(frame.PersonIDs) > 100 {
			return nil, ErrInvalid
		}
		if frame.Quality != nil && (math.IsNaN(*frame.Quality) || math.IsInf(*frame.Quality, 0) || *frame.Quality < 0 || *frame.Quality > 1) {
			return nil, ErrInvalid
		}
		ids := make(map[uuid.UUID]bool, len(frame.PersonIDs))
		for _, id := range frame.PersonIDs {
			if id == uuid.Nil || ids[id] {
				return nil, ErrInvalid
			}
			ids[id] = true
		}
		if previous, exists := unique[frame.AssetVersionID]; exists {
			if (previous.Quality == nil) != (frame.Quality == nil) || len(previous.PersonIDs) != len(ids) {
				return nil, ErrInvalid
			}
			if previous.Quality != nil && *previous.Quality != *frame.Quality {
				return nil, ErrInvalid
			}
			for id := range ids {
				if !people[frame.AssetVersionID][id] {
					return nil, ErrInvalid
				}
			}
			continue
		}
		unique[frame.AssetVersionID], people[frame.AssetVersionID] = frame, ids
	}
	coverage := make([]CaptureCoverage, 0, len(shots.Requirements))
	for _, requirement := range shots.Requirements {
		row := CaptureCoverage{RequirementID: requirement.ID}
		for version, frame := range unique {
			if frame.Quality == nil || *frame.Quality < requirement.MinQuality {
				continue
			}
			matches := true
			for _, person := range requirement.PersonIDs {
				if !people[version][person] {
					matches = false
					break
				}
			}
			if matches {
				row.ConfirmedFrames++
			}
		}
		coverage = append(coverage, row)
	}
	return coverage, nil
}

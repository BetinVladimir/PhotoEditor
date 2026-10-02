package v2input

import (
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
)

type CaptureCoverage struct {
	RequirementID   uuid.UUID `json:"requirement_id"`
	ConfirmedFrames int       `json:"confirmed_frames"`
}
type CaptureContext struct {
	SchemaVersion      int               `json:"schema_version"`
	SessionID          uuid.UUID         `json:"session_id"`
	ConfirmedPersonIDs []uuid.UUID       `json:"confirmed_person_ids"`
	ShotList           Ref               `json:"shot_list"`
	RecentContentSHAs  []string          `json:"recent_content_shas"`
	Consents           Ref               `json:"consents"`
	Coverage           []CaptureCoverage `json:"coverage"`
}
type CaptureShotRequirement struct {
	ID                 uuid.UUID   `json:"id"`
	Label              string      `json:"label"`
	PersonIDs          []uuid.UUID `json:"person_ids"`
	MinConfirmedFrames int         `json:"min_confirmed_frames"`
	MinQuality         float64     `json:"min_quality"`
}
type CaptureShotList struct {
	SchemaVersion int                      `json:"schema_version"`
	Requirements  []CaptureShotRequirement `json:"requirements"`
}

// CompileCaptureShotList validates the closed requirements model and returns
// the exact canonical body used for publication and immutable digest binding.
func CompileCaptureShotList(shots CaptureShotList) ([]byte, error) {
	if _, err := validateCaptureShots(shots); err != nil {
		return nil, err
	}
	return canonical.Marshal(shots)
}

// CompileCaptureContext requires one frozen coverage row per shot requirement.
// Counts must come from previously confirmed frames; this compiler never
// interprets the current automatic person match as a confirmed capture.
func CompileCaptureContext(c CaptureContext, shots CaptureShotList) ([]byte, error) {
	if c.SchemaVersion != 1 || shots.SchemaVersion != 1 || c.SessionID == uuid.Nil ||
		!validRef(c.ShotList) || !validRef(c.Consents) || c.ConfirmedPersonIDs == nil ||
		c.RecentContentSHAs == nil || c.Coverage == nil || shots.Requirements == nil ||
		len(c.ConfirmedPersonIDs) > 100 || len(c.RecentContentSHAs) > 5000 || len(shots.Requirements) > 200 || len(c.Coverage) != len(shots.Requirements) {
		return nil, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range c.ConfirmedPersonIDs {
		if id == uuid.Nil || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	for _, sha := range c.RecentContentSHAs {
		if !digestPattern.MatchString(sha) {
			return nil, ErrInvalid
		}
	}
	requirements, err := validateCaptureShots(shots)
	if err != nil {
		return nil, err
	}
	for _, row := range c.Coverage {
		if !requirements[row.RequirementID] || row.ConfirmedFrames < 0 || row.ConfirmedFrames > 5000 {
			return nil, ErrInvalid
		}
		delete(requirements, row.RequirementID)
	}
	// The context ref must refer to the exact immutable shot-list body supplied.
	hash, err := canonical.Hash(shots)
	if err != nil || hash != c.ShotList.SHA256 {
		return nil, ErrInvalid
	}
	return canonical.Marshal(c)
}

func validateCaptureShots(shots CaptureShotList) (map[uuid.UUID]bool, error) {
	if shots.SchemaVersion != 1 || shots.Requirements == nil || len(shots.Requirements) > 200 {
		return nil, ErrInvalid
	}
	requirements := map[uuid.UUID]bool{}
	for _, r := range shots.Requirements {
		if r.ID == uuid.Nil || requirements[r.ID] || len([]rune(r.Label)) < 1 || len([]rune(r.Label)) > 200 || r.PersonIDs == nil || len(r.PersonIDs) > 100 ||
			r.MinConfirmedFrames < 1 || r.MinConfirmedFrames > 100 || math.IsNaN(r.MinQuality) || math.IsInf(r.MinQuality, 0) || r.MinQuality < 0 || r.MinQuality > 1 {
			return nil, ErrInvalid
		}
		requirements[r.ID] = true
		people := map[uuid.UUID]bool{}
		for _, id := range r.PersonIDs {
			if id == uuid.Nil || people[id] {
				return nil, ErrInvalid
			}
			people[id] = true
		}
	}
	return requirements, nil
}

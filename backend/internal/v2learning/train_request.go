package v2learning

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type LearningPolicy struct {
	SchemaVersion   int         `json:"schema_version"`
	Mode            string      `json:"mode"`
	MinExamples     int         `json:"min_examples"`
	MinSessions     int         `json:"min_sessions"`
	HoldoutFraction float64     `json:"holdout_fraction"`
	Seed            int         `json:"seed"`
	Calibration     v2input.Ref `json:"calibration"`
}

type TrainCreate struct {
	ExpectedRevision string         `json:"expected_revision"`
	SessionIDs       []uuid.UUID    `json:"session_ids"`
	Policy           LearningPolicy `json:"policy"`
}

func (r *TrainCreate) UnmarshalJSON(body []byte) error {
	fields, err := closedLearningObject(body)
	if err != nil {
		return err
	}
	if len(fields) != 3 {
		return fmt.Errorf("TrainCreate requires three exact fields")
	}
	for _, key := range []string{"expected_revision", "session_ids", "policy"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("TrainCreate missing %s", key)
		}
	}
	policy, err := closedLearningObject(fields["policy"])
	if err != nil || len(policy) != 7 {
		return fmt.Errorf("invalid LearningPolicy")
	}
	for _, key := range []string{"schema_version", "mode", "min_examples",
		"min_sessions", "holdout_fraction", "seed", "calibration"} {
		if _, ok := policy[key]; !ok {
			return fmt.Errorf("LearningPolicy missing %s", key)
		}
	}
	calibration, err := closedLearningObject(policy["calibration"])
	if err != nil || len(calibration) != 2 {
		return fmt.Errorf("invalid calibration ref")
	}
	for _, key := range []string{"artifact_id", "sha256"} {
		if _, ok := calibration[key]; !ok {
			return fmt.Errorf("calibration ref missing %s", key)
		}
	}
	type wire TrainCreate
	var decoded wire
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	*r = TrainCreate(decoded)
	return nil
}

func validSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ValidateTrainCreate verifies the public request before the profile and
// session-specific SQL preflight. The return value is the parsed CAS revision.
func ValidateTrainCreate(req TrainCreate) (int64, error) {
	if len(req.ExpectedRevision) == 0 || len(req.ExpectedRevision) > 19 ||
		req.ExpectedRevision[0] == '0' || len(req.SessionIDs) < 6 ||
		len(req.SessionIDs) > 5000 || req.Policy.SchemaVersion != 1 ||
		(req.Policy.Mode != "photographer" && req.Policy.Mode != "person") ||
		req.Policy.MinExamples != 40 || req.Policy.MinSessions != 6 ||
		req.Policy.HoldoutFraction != 0.2 || req.Policy.Seed != 2026 ||
		req.Policy.Calibration.ArtifactID == uuid.Nil ||
		!validSHA(req.Policy.Calibration.SHA256) {
		return 0, fmt.Errorf("invalid TrainCreate")
	}
	for _, digit := range req.ExpectedRevision {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("invalid revision")
		}
	}
	revision, err := strconv.ParseInt(req.ExpectedRevision, 10, 64)
	if err != nil || revision < 1 {
		return 0, fmt.Errorf("invalid revision")
	}
	seen := make(map[uuid.UUID]struct{}, len(req.SessionIDs))
	for _, id := range req.SessionIDs {
		if id == uuid.Nil {
			return 0, fmt.Errorf("empty session ID")
		}
		if _, duplicate := seen[id]; duplicate {
			return 0, fmt.Errorf("duplicate session ID")
		}
		seen[id] = struct{}{}
	}
	return revision, nil
}

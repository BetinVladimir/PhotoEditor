package v2optical

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// DetectedPerspectiveLines is read-only evidence, never an Apply request.
type DetectedPerspectiveLines struct {
	SchemaVersion int                           `json:"schema_version"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Status        string                        `json:"status"`
	Lines         *PerspectiveLineSegments      `json:"lines"`
}

func ParseDetectedPerspectiveLines(raw []byte, expected v2input.Ref, version string) (DetectedPerspectiveLines, error) {
	var result DetectedPerspectiveLines
	if len(raw) < 2 || len(raw) > 1048576 || !validPerspectiveRef(expected) {
		return result, v2input.ErrInvalid
	}
	sum, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || sum != expected.SHA256 {
		return result, v2input.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return DetectedPerspectiveLines{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(result)
	if err != nil || !bytes.Equal(raw, exact) || result.SchemaVersion != 1 || result.Base.AssetVersionID.String() != version || result.Base.AssetVersionID == uuid.Nil || !validPerspectiveRef(result.Base.Source) || domainDigestInvalidDetected(result.Base.CanvasHash) {
		return DetectedPerspectiveLines{}, v2input.ErrInvalid
	}
	if result.Status == "unsupported" && result.Lines == nil {
		return result, nil
	}
	if result.Status != "available" || result.Lines == nil || len(result.Lines.Lines) > 128 {
		return DetectedPerspectiveLines{}, v2input.ErrInvalid
	}
	if _, _, err := result.Lines.Compile(); err != nil {
		return DetectedPerspectiveLines{}, err
	}
	for _, family := range []string{"horizontal", "vertical"} {
		minimum, maximum := 1.0, 0.0
		for _, line := range result.Lines.Lines {
			if line.Family != family {
				continue
			}
			center := (line.A.Y + line.B.Y) / 2
			if family == "vertical" {
				center = (line.A.X + line.B.X) / 2
			}
			if center < minimum {
				minimum = center
			}
			if center > maximum {
				maximum = center
			}
		}
		if maximum-minimum < 0.1 {
			return DetectedPerspectiveLines{}, v2input.ErrInvalid
		}
	}
	return result, nil
}
func domainDigestInvalidDetected(value string) bool {
	if len(value) != 64 {
		return true
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return true
		}
	}
	return false
}

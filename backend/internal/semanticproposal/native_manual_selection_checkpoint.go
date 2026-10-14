package semanticproposal

import (
	"context"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Original selected locators are immutable restart inputs, not manual authority.
// Every restored ref must still pass complete physical producer/pixel checks.
type nativeManualSelectionCheckpoint struct {
	Schema     int                           `json:"schema_version"`
	Parent     uuid.UUID                     `json:"parent_run_id"`
	RequestSHA string                        `json:"request_sha"`
	Project    uuid.UUID                     `json:"project_id"`
	Actor      uuid.UUID                     `json:"actor_id"`
	Session    uuid.UUID                     `json:"auth_session_id"`
	Source     v2input.SessionVersionBinding `json:"source"`
	Pixels     []v2input.Ref                 `json:"manual_pixels"`
	Diameter   [2]float64                    `json:"diameter_face_fraction"`
}

func compileNativeManualSelection(ctx context.Context, who auth.Identity, detector detectorDraftPlan, pixels []v2input.Ref, diameter [2]float64) ([]byte, string, error) {
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return nil, "", err
	}
	var config struct {
		Detector struct {
			Session uuid.UUID `json:"auth_session_id"`
		} `json:"v27_detector_prepare"`
	}
	if json.Unmarshal(detector.shell.Config, &config) != nil || config.Detector.Session != who.SessionID {
		return nil, "", v2input.ErrConflict
	}
	if who.UserID != detector.request.ActorID || who.SessionID == uuid.Nil {
		return nil, "", v2input.ErrInvalid
	}
	value := nativeManualSelectionCheckpoint{Schema: 1, Parent: detector.request.RunID, RequestSHA: detector.request.RequestSHA, Project: detector.shell.Project, Actor: who.UserID, Session: who.SessionID, Source: detector.request.Intent.Reference, Pixels: append([]v2input.Ref(nil), pixels...), Diameter: diameter}
	if err := validateNativeManualSelection(value); err != nil {
		return nil, "", err
	}
	body, err := canonical.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sha, err := canonical.Hash(value)
	return body, sha, err
}
func validateNativeManualSelection(value nativeManualSelectionCheckpoint) error {
	if value.Schema != 1 || value.Parent == uuid.Nil || value.Project == uuid.Nil || value.Actor == uuid.Nil || value.Session == uuid.Nil || value.Source.AssetVersionID == uuid.Nil || value.Source.Source.ArtifactID == uuid.Nil {
		return v2input.ErrInvalid
	}
	for _, digest := range []string{value.RequestSHA, value.Source.Source.SHA256, value.Source.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return v2input.ErrInvalid
		}
	}
	if err := validateInitialManualPixelSelection(value.Pixels); err != nil {
		return err
	}
	return validateInitialManualDiameter(value.Diameter)
}
func decodeNativeManualSelection(ctx context.Context, raw []byte, sha string, who auth.Identity, detector detectorDraftPlan) (nativeManualSelectionCheckpoint, error) {
	var zero, value nativeManualSelectionCheckpoint
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &value) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateNativeManualSelection(value); err != nil {
		return zero, err
	}
	expected, expectedSHA, err := compileNativeManualSelection(ctx, who, detector, value.Pixels, value.Diameter)
	if err != nil {
		return zero, err
	}
	actual, err := canonical.Bytes(json.RawMessage(raw))
	if err != nil {
		return zero, err
	}
	if string(actual) != string(expected) || expectedSHA != sha {
		return zero, v2input.ErrConflict
	}
	return value, nil
}

func validateInitialManualDiameter(diameter [2]float64) error {
	if math.IsNaN(diameter[0]) || math.IsNaN(diameter[1]) || math.IsInf(diameter[0], 0) || math.IsInf(diameter[1], 0) || diameter[0] <= 0 || diameter[1] < diameter[0] || diameter[1] > 1 {
		return v2input.ErrInvalid
	}
	return nil
}

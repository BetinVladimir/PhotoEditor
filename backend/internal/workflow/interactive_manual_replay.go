package workflow

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// Planned Params replace the original template. A retry validates the saved
// canonical payload rather than reading nonexistent template fields.
func verifyPlannedInteractiveManual(raw []byte, expectedSHA string, task uuid.UUID, image review.ArtifactRef, working review.WorkingImage) (bool, error) {
	actualSHA, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || actualSHA != expectedSHA {
		return false, errors.New("manual parameters hash mismatch")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false, err
	}
	if _, exists := fields["inline_vectors"]; !exists {
		return false, nil
	}
	fail := errors.New("invalid planned manual mask parameters")
	var params struct {
		Image    review.ArtifactRef `json:"image"`
		Vectors  json.RawMessage    `json:"inline_vectors"`
		ObjectID *uuid.UUID         `json:"object_id,omitempty"`
	}
	if json.Unmarshal(raw, &params) != nil || params.Image != image || params.ObjectID != nil && *params.ObjectID == uuid.Nil {
		return true, fail
	}
	expected, err := canonical.Marshal(params)
	if err != nil {
		return true, err
	}
	actual, err := canonical.Bytes(raw)
	if err != nil {
		return true, err
	}
	if !bytes.Equal(expected, actual) {
		return true, fail
	}
	vectors, err := canonical.Bytes(params.Vectors)
	if err != nil {
		return true, err
	}
	sha, err := canonical.Hash(json.RawMessage(vectors))
	if err != nil {
		return true, err
	}
	// Local validation pin grants no published artifact authority.
	ref := review.ArtifactRef{ID: uuid.NewSHA1(task, []byte("interactive.manual.vectors.validation")), SHA256: sha}
	return true, review.VerifyManualVectorsDocument(vectors, ref, working.Source(image))
}

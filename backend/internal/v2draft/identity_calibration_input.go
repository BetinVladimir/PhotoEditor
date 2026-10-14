package v2draft

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type IdentityCalibrationInput struct {
	Ref        v2input.Ref
	Raw        []byte
	Object     objectstore.Object
	DatasetSHA string
}

// LoadIdentityCalibrationInput derives the exact Calibration from task Params,
// validates its complete contract and repeats the versioned object locator.
// Its declared dataset must also match the live DBA policy in the commit tx.
func LoadIdentityCalibrationInput(ctx context.Context, scope IdentityResultScope,
	catalog IdentityResultInputCatalog, objects v2input.SessionAnalysisObjectReader,
	inputs IdentityResultInputs) (IdentityCalibrationInput, error) {
	var zero IdentityCalibrationInput
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || !isDocumentRef(inputs.Params.Calibration) ||
		!isDocumentRef(inputs.ParamsRef) {
		return zero, v2input.ErrInvalid
	}
	ref := inputs.Params.Calibration
	object, err := catalog.ResolveIdentityResultInput(ctx, scope, "calibration", ref)
	if err != nil {
		return zero, err
	}
	raw, err := readJSONArtifact(ctx, objects, object, ref)
	if err != nil {
		return zero, err
	}
	var header struct {
		DatasetSHA string `json:"dataset_sha"`
	}
	if json.Unmarshal(raw, &header) != nil ||
		!isDocumentRef(v2input.Ref{ArtifactID: ref.ArtifactID, SHA256: header.DatasetSHA}) ||
		v2input.VerifySemanticCalibration(ref, raw, inputs.Params.ModelLockSHA, header.DatasetSHA) != nil {
		return zero, v2input.ErrInvalid
	}
	current, err := catalog.ResolveIdentityResultInput(ctx, scope, "calibration", ref)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrExpired
	}
	return IdentityCalibrationInput{Ref: ref, Raw: raw, Object: object,
		DatasetSHA: header.DatasetSHA}, nil
}

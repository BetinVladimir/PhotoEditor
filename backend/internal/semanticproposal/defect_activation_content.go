package semanticproposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Text bodies retain exact original bytes when decoded by SQL; JSON embedding
// must not normalize their numeric/string representation before hash checks.
type defectActivationContent struct {
	SchemaVersion      int         `json:"schema_version"`
	WorkingImage       v2input.Ref `json:"working_image"`
	OriginalParams     v2input.Ref `json:"original_params"`
	Params             v2input.Ref `json:"params"`
	WorkingImageText   string      `json:"working_image_text"`
	OriginalParamsText string      `json:"original_params_text"`
	ParamsText         string      `json:"params_text"`
}

// Owns bounded exact content for the dedicated SQL admission proof. No object IO,
// task creation or queue authority; caller separately locks original provenance.
func marshalDefectActivationContent(ctx context.Context, plan defectPreparationDraftPlan) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	original := plan.execution.execution.face.originProof.objects[0].ref
	if original != plan.execution.execution.face.origin.Params {
		return nil, v2input.ErrInvalid
	}
	content := defectActivationContent{SchemaVersion: 1, WorkingImage: plan.request.Intent.Reference.Source, OriginalParams: original, Params: plan.params.params, WorkingImageText: plan.workingImage, OriginalParamsText: plan.originalParams, ParamsText: plan.params.preparation.params.params}
	// These are three distinct immutable documents, even when Params bytes match.
	if content.WorkingImage.ArtifactID == content.OriginalParams.ArtifactID || content.WorkingImage.ArtifactID == content.Params.ArtifactID || content.OriginalParams.ArtifactID == content.Params.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	for _, body := range []struct {
		ref  v2input.Ref
		text string
	}{{content.WorkingImage, content.WorkingImageText}, {content.OriginalParams, content.OriginalParamsText}, {content.Params, content.ParamsText}} {
		if body.ref.ArtifactID == uuid.Nil || len(body.text) < 2 || len(body.text) > 1048576 {
			return nil, v2input.ErrInvalid
		}
		sum := sha256.Sum256([]byte(body.text))
		if hex.EncodeToString(sum[:]) != body.ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
	}
	image, err := review.ParseWorkingImage([]byte(content.WorkingImageText))
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return nil, v2input.ErrInvalid
	}
	if _, err := v2input.ResolveSemanticSourceMetadata(plan.request.Intent.Reference, []byte(content.WorkingImageText)); err != nil {
		return nil, err
	}
	source := image.Source(review.ArtifactRef{ID: content.WorkingImage.ArtifactID, SHA256: content.WorkingImage.SHA256})
	faces, err := review.DefectProposalFaceInputsFromParams(review.ArtifactRef{ID: original.ArtifactID, SHA256: original.SHA256}, []byte(content.OriginalParamsText), source)
	if err != nil {
		return nil, err
	}
	masks, err := review.DefectProposalMaskInputs(review.ArtifactRef{ID: original.ArtifactID, SHA256: original.SHA256}, []byte(content.OriginalParamsText), source)
	if err != nil {
		return nil, err
	}
	required := make([]review.ProtectionMask, 0)
	for _, mask := range masks {
		if mask.Protected {
			required = append(required, mask)
		}
	}
	nativeRef := review.ArtifactRef{ID: content.Params.ArtifactID, SHA256: content.Params.SHA256}
	raw := []byte(content.ParamsText)
	canon, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canon, raw) {
		return nil, v2input.ErrInvalid
	}
	if err := review.VerifyDefectQualifiedFaceBindings(ctx, nativeRef, raw, source, faces); err != nil {
		return nil, err
	}
	if err := review.VerifyDefectRequiredProtections(ctx, nativeRef, raw, source, required); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return canonical.Marshal(content)
}

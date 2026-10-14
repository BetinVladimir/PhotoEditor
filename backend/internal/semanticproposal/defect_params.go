package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// PublishedDefectParams retains original compiled dependencies alongside the
// publisher-returned Params reference. It does not authorize workflow admission.
type PublishedDefectParams struct {
	Ref         v2input.Ref
	Inputs      []review.ArtifactRef
	ManualRoots []review.ArtifactRef
}

// CompileAndPublishDefectParams uses the existing owner-scoped Params publication
// boundary. The caller must first select/qualify the complete original sources;
// the publisher must enforce an authorized graph owner and publication fencing.
// The current SQL publisher does not authorize arbitrary defect task UUIDs.
func CompileAndPublishDefectParams(ctx context.Context, publisher v2input.DocumentPublisher, scope artifactjson.Scope, imageRef review.ArtifactRef, imageBody []byte, faces []review.DefectProposalFaceInput, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection) (PublishedDefectParams, error) {
	empty := PublishedDefectParams{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil || scope.Purpose != "task_params" || scope.Tenant == uuid.Nil || scope.Run == uuid.Nil || scope.Owner == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	prepared, err := PrepareDefectParams(ctx, imageRef, imageBody, faces, diameter, permanent, additional, manuals)
	if err != nil {
		return empty, err
	}
	return PublishPreparedDefectParams(ctx, publisher, scope, prepared)
}

// PreparedDefectParams owns compiler output across qualification and publication
// phases. It contains no catalog, lease or admission authority.
type PreparedDefectParams struct {
	params, digest, inputs, roots string
}

func PrepareDefectParams(ctx context.Context, imageRef review.ArtifactRef, imageBody []byte, faces []review.DefectProposalFaceInput, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection) (PreparedDefectParams, error) {
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(ctx, imageRef, imageBody, faces, diameter, permanent, additional, manuals)
	if err != nil {
		return PreparedDefectParams{}, err
	}
	return freezeCompiledDefectParams(ctx, compiled)
}

// PublishPreparedDefectParams publishes a previously compiled immutable payload.
// The configured publisher must enforce original owner/lease authority before
// reserve and after object IO; this function does not create graph ownership.
func PublishPreparedDefectParams(ctx context.Context, publisher v2input.DocumentPublisher, scope artifactjson.Scope, prepared PreparedDefectParams) (PublishedDefectParams, error) {
	empty := PublishedDefectParams{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil || scope.Purpose != "task_params" || scope.Tenant == uuid.Nil || scope.Run == uuid.Nil || scope.Owner == uuid.Nil || len(prepared.params) < 2 || len(prepared.params) > 1048576 || len(prepared.inputs) > 4194304 || len(prepared.roots) > 4194304 {
		return empty, v2input.ErrInvalid
	}
	digest := sha256.Sum256([]byte(prepared.params))
	if prepared.digest != hex.EncodeToString(digest[:]) {
		return empty, v2input.ErrInvalid
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.inputs), &inputs) != nil || json.Unmarshal([]byte(prepared.roots), &roots) != nil || len(inputs) < 2 || len(inputs) > 4096 || roots == nil {
		return empty, v2input.ErrInvalid
	}
	ref, err := publisher.Publish(ctx, scope, []byte(prepared.params))
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != prepared.digest {
		return empty, v2input.ErrInvalid
	}
	for _, input := range inputs {
		if input.ID == ref.ID {
			return empty, v2input.ErrInvalid
		}
	}
	return PublishedDefectParams{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Inputs: inputs, ManualRoots: roots}, nil
}

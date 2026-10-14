package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ResolveLocalPortraitTaskInputEvidence produces one scoped traversal for local
// operation Params and its original manual raster dependencies. Caller owns
// graph admission, live lease checks and binary raster verification.
func (s ClosureResolver) ResolveLocalPortraitTaskInputEvidence(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, binding v2input.SessionVersionBinding, localRef v2input.Ref, localRaw, sourceRaw []byte,
	rootRef v2input.Ref, rootRaw []byte, originalParams v2input.Ref, originalRaw, vectorsRaw []byte,
	targetRef, guardRef v2input.Ref) (TaskInputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return TaskInputEvidence{}, err
	}
	if !isDocumentRef(localRef) || len(localRaw) < 2 || len(localRaw) > 65536 {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	sum := sha256.Sum256(localRaw)
	if hex.EncodeToString(sum[:]) != localRef.SHA256 {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	_, handled, err := localPortraitParamRefs(localRaw, "TaskInputs.local_params", localRef)
	if err != nil || !handled {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	var header struct {
		Base      v2input.SessionVersionBinding `json:"base"`
		Mask      v2input.Ref                   `json:"mask"`
		Protected v2input.Ref                   `json:"protected"`
	}
	if json.Unmarshal(localRaw, &header) != nil || header.Base != binding || header.Mask != targetRef || header.Protected != guardRef {
		return TaskInputEvidence{}, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{binding.Source, rootRef, originalParams, targetRef, guardRef} {
		if ref.ArtifactID == localRef.ArtifactID {
			return TaskInputEvidence{}, v2input.ErrInvalid
		}
	}
	local := LayerRef{Path: "TaskInputs.local_params", Contract: "Params", Ref: localRef}
	return s.resolveManualRasterTaskInputEvidence(ctx, who, tenant, binding, sourceRaw, rootRef, rootRaw, originalParams, originalRaw, vectorsRaw, targetRef, guardRef, &local)
}

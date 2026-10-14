package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Pure exact declaration-to-original-pin matching; this grants no producer
// authority and performs no pixel IO. Caller must verify original proofs first.
func nativeInputManualDeclaredProtectionPins(ctx context.Context, selection nativeInputManualSelection, declared []review.ProtectionMask) ([]defectEvidenceObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(selection.proofs) < 1 || len(selection.proofs) > 64 || len(declared) != len(selection.policy.Protections) || selection.policy.Permanent != nil {
		return nil, v2input.ErrConflict
	}
	required := make(map[v2input.Ref]defectEvidenceObject, len(selection.proofs))
	for _, proof := range selection.proofs {
		child := proof.original().children[0]
		if child.ref.ArtifactID == uuid.Nil {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(child.ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		if old, exists := required[child.ref]; exists && old != child {
			return nil, v2input.ErrConflict
		}
		required[child.ref] = child
	}
	out := make([]defectEvidenceObject, 0, len(declared))
	for i, mask := range declared {
		if !reflect.DeepEqual(mask, selection.policy.Protections[i]) {
			return nil, v2input.ErrConflict
		}
		ref := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
		child, ok := required[ref]
		if !ok {
			return nil, v2input.ErrConflict
		}
		out = append(out, child)
		delete(required, ref)
	}
	if len(required) != 0 {
		return nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

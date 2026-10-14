package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// PublishTargetSnapshot reads live source-bound recipes and persists the same
// immutable snapshot on retry. It returns no reference on a changed inventory.
// Graph admission still must lock snapshot/recipe pins in its final transaction.
func (s SQLTransferSourceReader) PublishTargetSnapshot(ctx context.Context, publisher v2draft.Publisher, who auth.Identity, request Request, key string, published PublishedIntent) (v2input.Ref, []byte, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, nil, err
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	intentSHA, err := canonical.Hash(owned.Intent)
	if err != nil || who.UserID != owned.ActorID || who.SessionID == uuid.Nil || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != intentSHA {
		return v2input.Ref{}, nil, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, owned)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	if !sameTransferInventory(before, published.Inventory) {
		return v2input.Ref{}, nil, v2input.ErrConflict
	}
	snapshot, body, err := s.ReadTargetSnapshot(ctx, who, owned)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.input_snapshot"))
	ref, err := publisher.PublishSemanticSnapshot(ctx, who, owned.TenantID, snapshot.Items[0].Binding.AssetVersionID, document, body)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	after, err := ReadInventory(ctx, s.DB, who, owned)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	if !sameTransferInventory(before, after) {
		return v2input.Ref{}, nil, v2input.ErrConflict
	}
	if _, err := v2input.DecodePinnedSessionInputSnapshot(ref, body); err != nil {
		return v2input.Ref{}, nil, err
	}
	// Recipe selection can change during the immutable PUT. Re-read and compare
	// exact canonical snapshot bytes rather than accepting the old stream heads.
	_, current, err := s.ReadTargetSnapshot(ctx, who, owned)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	if string(current) != string(body) {
		return v2input.Ref{}, nil, v2input.ErrConflict
	}
	return ref, body, nil
}

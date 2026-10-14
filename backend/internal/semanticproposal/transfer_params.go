package semanticproposal

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// PublishTransferParams preserves completed documents after partial failure for
// same-key recovery. It does not qualify source inputs or admit tasks.
func PublishTransferParams(ctx context.Context, publisher v2draft.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, modelSHA string, calibration v2input.Ref) ([]v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return nil, err
	}
	intentSHA, err := canonical.Hash(owned.Intent)
	targets := append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)
	if err != nil || who.UserID != owned.ActorID || who.SessionID == uuid.Nil || request.RunID != owned.RunID || request.WorkflowID != owned.WorkflowID || request.RequestSHA != owned.RequestSHA || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != intentSHA || published.Inventory.Scope.ProjectID == uuid.Nil || published.Inventory.Scope.ItemCount != len(targets) || len(targets) < 1 || len(targets) > 5000 {
		return nil, v2input.ErrInvalid
	}
	// Validate and own the entire batch before object IO.
	bodies := make([][]byte, len(targets))
	hashes := make([]string, len(targets))
	seen := map[uuid.UUID]bool{}
	for i, target := range targets {
		if seen[target.AssetVersionID] {
			return nil, v2input.ErrInvalid
		}
		seen[target.AssetVersionID] = true
		params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: owned.Intent.Reference, Target: target, ModelLockSHA: modelSHA, Calibration: calibration}
		body, err := canonical.Marshal(params)
		if err != nil {
			return nil, err
		}
		if _, err := v2input.DecodeSemanticTransferParams(body); err != nil {
			return nil, err
		}
		digest, err := canonical.Hash(params)
		if err != nil {
			return nil, err
		}
		bodies[i], hashes[i] = body, digest
	}

	live, err := ReadInventory(ctx, publisher.DB, who, owned)
	if err != nil {
		return nil, err
	}
	if !sameTransferInventory(live, published.Inventory) {
		return nil, v2input.ErrConflict
	}
	refs := make([]v2input.Ref, len(targets))
	for i, body := range bodies {
		document := uuid.NewSHA1(owned.RunID, []byte(fmt.Sprintf("semantic.transfer.params:%d", i)))
		ref, err := publisher.PublishSemanticTransferParams(ctx, who, owned.TenantID, targets[i].AssetVersionID, document, body)
		if err != nil {
			return nil, err
		}
		if ref.ArtifactID == uuid.Nil || ref.SHA256 != hashes[i] {
			return nil, v2input.ErrInvalid
		}
		refs[i] = ref
	}

	live, err = ReadInventory(ctx, publisher.DB, who, owned)
	if err != nil {
		return nil, err
	}
	if !sameTransferInventory(live, published.Inventory) {
		return nil, v2input.ErrConflict
	}
	return refs, nil
}

func sameTransferInventory(a, b CatalogInventory) bool {
	if a.Scope != b.Scope || len(a.Targets) != len(b.Targets) {
		return false
	}
	for i := range a.Targets {
		if a.Targets[i] != b.Targets[i] {
			return false
		}
	}
	return true
}

package semanticproposal

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type PublishedIntent struct {
	Ref       v2input.Ref
	Inventory CatalogInventory
}

// PublishIntent checks live inventory before and after immutable object IO.
// It neither admits tasks nor replaces final locked catalog verification.
func PublishIntent(ctx context.Context, publisher v2draft.Publisher, who auth.Identity, request Request) (PublishedIntent, error) {
	var empty PublishedIntent
	body, err := canonical.Marshal(request.Intent)
	if err != nil || !bytes.Equal(body, request.Body) || request.RunID == uuid.Nil || request.WorkflowID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(request.RunID)
	if err != nil || expectedWorkflow != request.WorkflowID {
		return empty, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(struct {
		Tenant uuid.UUID                  `json:"tenant_id"`
		Actor  uuid.UUID                  `json:"actor_id"`
		Intent v2input.SemanticEditIntent `json:"intent"`
	}{request.TenantID, request.ActorID, request.Intent})
	if err != nil || digest != request.RequestSHA {
		return empty, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, publisher.DB, who, request)
	if err != nil {
		return empty, err
	}
	document := uuid.NewSHA1(request.RunID, []byte("semantic.edit_intent"))
	ref, err := publisher.PublishSemanticIntent(ctx, who, request.TenantID, request.Intent.Reference.AssetVersionID, document, body)
	if err != nil {
		return empty, err
	}
	intentSHA, err := canonical.Hash(request.Intent)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != intentSHA {
		return empty, v2input.ErrInvalid
	}
	after, err := ReadInventory(ctx, publisher.DB, who, request)
	if err != nil {
		return empty, err
	}
	if before.Scope != after.Scope || len(before.Targets) != len(after.Targets) {
		return empty, v2input.ErrConflict
	}
	for i := range before.Targets {
		if before.Targets[i] != after.Targets[i] {
			return empty, v2input.ErrConflict
		}
	}
	return PublishedIntent{Ref: ref, Inventory: after}, nil
}

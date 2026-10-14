package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ReadSelectedMask accepts the uploaded user-mask origin supported by proposal
// inventory. Pixel NPY validation and protection masks require separate resolvers.
func (s SQLTransferSourceReader) ReadSelectedMask(ctx context.Context, who auth.Identity, request Request) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if who.UserID != request.ActorID {
		return nil, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, err
	}
	if before.Scope.ProjectID != s.ProjectID {
		return nil, v2input.ErrInvalid
	}
	image, err := s.Read(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return nil, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	node := v2draft.LayerRef{Ref: request.Intent.ReferenceMask, Contract: "MaskRef", CanvasHash: request.Intent.Reference.CanvasHash}
	object, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return nil, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, request.Intent.ReferenceMask)
	if err != nil {
		return nil, err
	}
	if _, err := v2input.ResolveSemanticMaskPixels(request.Intent.ReferenceMask, body, request.Intent.Reference, image, false); err != nil {
		return nil, err
	}
	current, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, v2input.ErrConflict
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, err
	}
	if !sameTransferInventory(before, after) {
		return nil, v2input.ErrConflict
	}
	return json.RawMessage(body), nil
}

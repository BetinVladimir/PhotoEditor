package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type VerifiedPersonMask struct {
	Root, Pixels v2input.Ref
	Body         json.RawMessage
}

// ReadPersonMask proves exact operation origin, pinned model metadata and mask
// bytes before policy compilation. Model/report qualification and automatic
// analysis inventory selection remain separate authority requirements.
func (s SQLTransferSourceReader) ReadPersonMask(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root v2input.Ref, modelLock string) (VerifiedPersonMask, error) {
	empty := VerifiedPersonMask{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if _, err := domain.ParseDigest(modelLock); err != nil {
		return empty, v2input.ErrInvalid
	}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return empty, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	node := v2draft.LayerRef{Ref: root, Contract: "MaskRef", CanvasHash: binding.CanvasHash}
	object, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return empty, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, root)
	if err != nil {
		return empty, err
	}
	pixels, err := v2input.ResolveSemanticPersonMaskPixels(root, body, binding, image, modelLock)
	if err != nil {
		return empty, err
	}
	if err := s.VerifyPersonMaskOrigin(ctx, who, tenantID, binding, root, pixels); err != nil {
		return empty, err
	}
	childNode := v2draft.LayerRef{Ref: pixels, Contract: "MaskPixels", ParentRef: root}
	child, err := catalog.Lookup(ctx, who, tenantID, childNode)
	if err != nil {
		return empty, err
	}
	if child.ContentType != "application/x-npy" && child.ContentType != "application/octet-stream" {
		return empty, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, child)
	if err != nil {
		return empty, err
	}
	verifyErr := masknpy.VerifyFloat32MaskNPY(ctx, stream, child.Bytes, pixels.SHA256, metadata.Width, metadata.Height)
	closeErr := stream.Close()
	if verifyErr != nil {
		return empty, verifyErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	current, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return empty, err
	}
	if current != object {
		return empty, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, tenantID, childNode)
	if err != nil {
		return empty, err
	}
	if current != child {
		return empty, v2input.ErrConflict
	}
	if err := s.VerifyPersonMaskOrigin(ctx, who, tenantID, binding, root, pixels); err != nil {
		return empty, err
	}
	return VerifiedPersonMask{Root: root, Pixels: pixels, Body: append(json.RawMessage(nil), body...)}, nil
}

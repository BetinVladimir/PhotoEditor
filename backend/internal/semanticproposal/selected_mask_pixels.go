package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifySelectedMaskPixels validates the exact mask child through parent-scoped
// catalog authority and streaming float32 NPY shape/hash/finite/unit checks.
// Nonempty selection and overlap with protection need paired mask verification.
func (s SQLTransferSourceReader) VerifySelectedMaskPixels(ctx context.Context, who auth.Identity, request Request) (v2input.Ref, error) {
	body, err := s.ReadSelectedMask(ctx, who, request)
	if err != nil {
		return v2input.Ref{}, err
	}
	mask, err := review.ParseMaskRef(body)
	if err != nil {
		return v2input.Ref{}, err
	}
	ref := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	node := v2draft.LayerRef{Ref: ref, Contract: "MaskPixels", ParentRef: request.Intent.ReferenceMask}
	object, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, err
	}
	if object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, object)
	if err != nil {
		return v2input.Ref{}, err
	}
	verifyErr := masknpy.VerifyFloat32MaskNPY(ctx, stream, object.Bytes, ref.SHA256, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	closeErr := stream.Close()
	if verifyErr != nil {
		return v2input.Ref{}, verifyErr
	}
	if closeErr != nil {
		return v2input.Ref{}, closeErr
	}
	current, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, err
	}
	if current != object {
		return v2input.Ref{}, v2input.ErrConflict
	}
	if _, err := ReadInventory(ctx, s.DB, who, request); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}

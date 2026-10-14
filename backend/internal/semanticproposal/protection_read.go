package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type VerifiedProtection struct {
	Root, Pixels v2input.Ref
	Body         json.RawMessage
}

// ReadProtection validates a server-selected protection root. Selection of the
// required person/defect policy is separate; a generic protected mask is not a
// substitute for the required policy. No synthetic fallback is generated.
func (s SQLTransferSourceReader) ReadProtection(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root v2input.Ref) (VerifiedProtection, error) {
	result, _, err := s.readProtectionPreparationProof(ctx, who, tenantID, binding, root)
	return result, err
}

func (s SQLTransferSourceReader) readProtectionPreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root v2input.Ref) (VerifiedProtection, protectionPreparationProof, error) {
	empty := VerifiedProtection{}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	node := v2draft.LayerRef{Ref: root, Contract: "MaskRef", CanvasHash: binding.CanvasHash}
	object, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, root)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(root, body, binding, image, true)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, root, pixels); err != nil {
		return empty, protectionPreparationProof{}, err
	}
	childNode := v2draft.LayerRef{Ref: pixels, Contract: "MaskPixels", ParentRef: root}
	child, err := catalog.Lookup(ctx, who, tenantID, childNode)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	if child.ContentType != "application/x-npy" && child.ContentType != "application/octet-stream" {
		return empty, protectionPreparationProof{}, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, child)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	verifyErr := masknpy.VerifyFloat32MaskNPY(ctx, stream, child.Bytes, pixels.SHA256, metadata.Width, metadata.Height)
	closeErr := stream.Close()
	if verifyErr != nil {
		return empty, protectionPreparationProof{}, verifyErr
	}
	if closeErr != nil {
		return empty, protectionPreparationProof{}, closeErr
	}
	current, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	if current != object {
		return empty, protectionPreparationProof{}, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, tenantID, childNode)
	if err != nil {
		return empty, protectionPreparationProof{}, err
	}
	if current != child {
		return empty, protectionPreparationProof{}, v2input.ErrConflict
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, root, pixels); err != nil {
		return empty, protectionPreparationProof{}, err
	}
	proof := protectionPreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, root: defectEvidenceObject{ref: root, object: object}, pixels: defectEvidenceObject{ref: pixels, object: child}}
	if !validProtectionPreparationScope(s, who, tenantID, binding, proof) {
		return empty, protectionPreparationProof{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, protectionPreparationProof{}, err
	}
	return VerifiedProtection{Root: root, Pixels: pixels, Body: append(json.RawMessage(nil), body...)}, proof, nil
}

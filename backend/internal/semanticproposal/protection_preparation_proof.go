package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Selected original protected mask pins only. Required policy selection and
// coverage, complete source closure and admission locks are separate.
type protectionPreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	binding                         v2input.SessionVersionBinding
	root, pixels                    defectEvidenceObject
}

func validProtectionPreparationScope(s SQLTransferSourceReader, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, p protectionPreparationProof) bool {
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || p.tenant != tenant || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding {
		return false
	}
	for _, sha := range []string{binding.Source.SHA256, binding.CanvasHash, p.root.ref.SHA256, p.pixels.ref.SHA256} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return false
		}
	}
	seen := map[uuid.UUID]bool{binding.Source.ArtifactID: true}
	for i, pin := range [2]defectEvidenceObject{p.root, p.pixels} {
		if pin.ref.ArtifactID == uuid.Nil || seen[pin.ref.ArtifactID] || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.SHA256 != pin.ref.SHA256 {
			return false
		}
		seen[pin.ref.ArtifactID] = true
		if i == 0 {
			if pin.object.ContentType != "application/json" || pin.object.Bytes < 2 || pin.object.Bytes > 1048576 {
				return false
			}
		} else if (pin.object.ContentType != "application/x-npy" && pin.object.ContentType != "application/octet-stream") || pin.object.Bytes < 32 || pin.object.Bytes > 800065548 {
			return false
		}
	}
	return true
}
func (s SQLTransferSourceReader) recheckProtectionPreparationProof(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, p protectionPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validProtectionPreparationScope(s, who, tenant, binding, p) {
		return v2input.ErrInvalid
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	nodes := [2]v2draft.LayerRef{{Ref: p.root.ref, Contract: "MaskRef", CanvasHash: binding.CanvasHash}, {Ref: p.pixels.ref, Contract: "MaskPixels", ParentRef: p.root.ref}}
	for i, pin := range [2]defectEvidenceObject{p.root, p.pixels} {
		obj, err := catalog.Lookup(ctx, who, tenant, nodes[i])
		if err != nil {
			return err
		}
		if obj != pin.object {
			return v2input.ErrConflict
		}
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenant, binding, p.root.ref, p.pixels.ref); err != nil {
		return err
	}
	return ctx.Err()
}

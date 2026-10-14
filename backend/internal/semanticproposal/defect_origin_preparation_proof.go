package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Exact privately retained canonical origin metadata. Content verification and
// transaction producer/evidence locks are separate requirements.
type defectOriginPreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	binding                         v2input.SessionVersionBinding
	pins                            DefectOriginPins
	objects                         [5]defectEvidenceObject
}

func (s SQLTransferSourceReader) readDefectOriginPreparationProof(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) (defectOriginPreparationProof, error) {
	var zero defectOriginPreparationProof
	entries, err := s.defectEvidenceObjects(ctx, who, tenant, binding, pins)
	if err != nil {
		return zero, err
	}
	if len(entries) != 5 {
		return zero, v2input.ErrInvalid
	}
	proof := defectOriginPreparationProof{tenant: tenant, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, pins: pins}
	copy(proof.objects[:], entries)
	if !s.validDefectOriginPreparationProof(who, tenant, binding, proof) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return proof, nil
}
func (s SQLTransferSourceReader) validDefectOriginPreparationProof(who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, p defectOriginPreparationProof) bool {
	if binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || p.tenant != tenant || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding {
		return false
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return false
		}
	}
	seen := map[uuid.UUID]bool{}
	for i, ref := range []v2input.Ref{p.pins.Params, p.pins.Proposal, p.pins.Report, p.pins.Labels, p.pins.Protection} {
		entry := p.objects[i]
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] || entry.ref != ref || entry.object.SHA256 != ref.SHA256 || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" {
			return false
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return false
		}
		media, min, max := "application/json", int64(2), int64(1048576)
		if i >= 3 {
			media, min, max = "application/octet-stream", 32, 800065548
		}
		if entry.object.ContentType != media || entry.object.Bytes < min || entry.object.Bytes > max {
			return false
		}
		seen[ref.ArtifactID] = true
	}
	return true
}
func (s SQLTransferSourceReader) recheckDefectOriginPreparationProof(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof defectOriginPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.validDefectOriginPreparationProof(who, tenant, binding, proof) {
		return v2input.ErrInvalid
	}
	current, err := s.readDefectOriginPreparationProof(ctx, who, tenant, binding, proof.pins)
	if err != nil {
		return err
	}
	if current != proof {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

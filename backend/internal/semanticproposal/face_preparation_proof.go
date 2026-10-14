package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Private selected preflight evidence only. RGB/ICC closure and final transaction
// evidence/policy locks remain separate; recheck never grants queued admission.
type facePreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	binding                         v2input.SessionVersionBinding
	objects                         [2][2]defectEvidenceObject
	qualification                   [2]personEvidenceObject
	expected                        string
}

func validFacePreparationProofScope(s SQLTransferSourceReader, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, p facePreparationProof) bool {
	if tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || s.Bucket == "" || p.tenant != tenantID || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding || len(p.expected) < 2 || len(p.expected) > 4194304 {
		return false
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return false
		}
	}
	seen := map[uuid.UUID]bool{binding.Source.ArtifactID: true}
	for _, pair := range p.objects {
		if !validFacePreparationObjects(pair, [2]string{p.bucket, p.bucket}, p.bucket, binding.Source) {
			return false
		}
		for _, pin := range pair {
			if seen[pin.ref.ArtifactID] || pin.object.SHA256 != pin.ref.SHA256 || pin.object.ContentType != "application/json" {
				return false
			}
			seen[pin.ref.ArtifactID] = true
		}
	}
	for _, q := range p.qualification {
		if q.ref.ArtifactID == uuid.Nil || seen[q.ref.ArtifactID] || q.contract != "QualificationReport" || q.object.Key == "" || q.object.VersionID == "" || q.object.VersionID == "null" || q.object.Bytes < 2 || q.object.Bytes > 8388608 || q.object.SHA256 != q.ref.SHA256 || q.object.ContentType != "application/json" {
			return false
		}
		if _, err := domain.ParseDigest(q.ref.SHA256); err != nil {
			return false
		}
		seen[q.ref.ArtifactID] = true
	}
	return true
}
func (s SQLTransferSourceReader) recheckFacePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof facePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validFacePreparationProofScope(s, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	var expected [2]v2qualification.Expected
	if json.Unmarshal([]byte(proof.expected), &expected) != nil {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != proof.expected {
		return v2input.ErrInvalid
	}
	refs := [2]v2input.Ref{proof.objects[0][0].ref, proof.objects[1][0].ref}
	if err := s.verifyFacePreparationProfiles(ctx, who, tenantID, binding, refs[0], refs[1], expected); err != nil {
		return err
	}
	for i, ref := range refs {
		current, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
		if err != nil {
			return err
		}
		if current != proof.objects[i] {
			return v2input.ErrConflict
		}
		q, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], proof.qualification[i].ref)
		if err != nil {
			return err
		}
		if q != proof.qualification[i] {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}

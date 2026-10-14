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

// Private value-owned selected parser evidence. No maps/pointers escape; source
// RGB/ICC closure and final admission locks are separate obligations.
type faceParsePreparationProof struct {
	evidence facePreparationProof
	pixels   defectEvidenceObject
}

func validFaceParsePreparationProofScope(s SQLTransferSourceReader, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, p faceParsePreparationProof) bool {
	if !validFacePreparationProofScope(s, who, tenantID, binding, p.evidence) {
		return false
	}
	pin := p.pixels
	if pin.ref.ArtifactID == uuid.Nil || pin.ref.ArtifactID == binding.Source.ArtifactID || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.Bytes < 32 || pin.object.Bytes > 400065548 || pin.object.SHA256 != pin.ref.SHA256 || pin.object.ContentType != "application/octet-stream" {
		return false
	}
	if _, err := domain.ParseDigest(pin.ref.SHA256); err != nil {
		return false
	}
	for _, pair := range p.evidence.objects {
		for _, obj := range pair {
			if obj.ref.ArtifactID == pin.ref.ArtifactID {
				return false
			}
		}
	}
	for _, q := range p.evidence.qualification {
		if q.ref.ArtifactID == pin.ref.ArtifactID {
			return false
		}
	}
	var expected [2]v2qualification.Expected
	if json.Unmarshal([]byte(p.evidence.expected), &expected) != nil {
		return false
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != p.evidence.expected {
		return false
	}
	for i, value := range expected {
		if value.Operation != [2]string{"face.parse", "face.detect"}[i] || value.Version != "1" || value.Backend != p.evidence.qualification[i].backend {
			return false
		}
	}
	return true
}
func (s SQLTransferSourceReader) recheckFaceParsePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, p faceParsePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validFaceParsePreparationProofScope(s, who, tenantID, binding, p) {
		return v2input.ErrInvalid
	}
	var expected [2]v2qualification.Expected
	if json.Unmarshal([]byte(p.evidence.expected), &expected) != nil {
		return v2input.ErrInvalid
	}
	parse, detect := p.evidence.objects[0][0].ref, p.evidence.objects[1][0].ref
	if err := s.verifyFaceParsePreparationProfiles(ctx, who, tenantID, binding, parse, detect, p.pixels.ref, expected); err != nil {
		return err
	}
	for i, ref := range [2]v2input.Ref{parse, detect} {
		objects, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
		if err != nil {
			return err
		}
		q, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], p.evidence.qualification[i].ref)
		if err != nil {
			return err
		}
		if objects != p.evidence.objects[i] || q != p.evidence.qualification[i] {
			return v2input.ErrConflict
		}
	}
	pixels, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, detect, p.pixels.ref)
	if err != nil {
		return err
	}
	if pixels != p.pixels {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

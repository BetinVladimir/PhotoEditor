package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

func marshalFaceParseEvidencePins(proof faceParsePreparationProof) ([]byte, error) {
	p := proof.evidence
	s := SQLTransferSourceReader{ProjectID: p.project, Bucket: p.bucket}
	if !validFaceParsePreparationProofScope(s, auth.Identity{UserID: p.actor, SessionID: p.session}, p.tenant, p.binding, proof) {
		return nil, v2input.ErrInvalid
	}
	roles := [2][2]string{{"face_labels_ref", "parse_params"}, {"face_boxes", "detector_params"}}
	pins := make([]manualEvidencePin, 0, 5)
	for i, pair := range p.objects {
		for j, obj := range pair {
			pins = append(pins, manualEvidencePin{roles[i][j], obj.ref.ArtifactID, obj.ref.SHA256, p.bucket, obj.object.Key, obj.object.VersionID, obj.object.Bytes, obj.object.ContentType})
		}
	}
	obj := proof.pixels
	pins = append(pins, manualEvidencePin{"face_labels", obj.ref.ArtifactID, obj.ref.SHA256, p.bucket, obj.object.Key, obj.object.VersionID, obj.object.Bytes, obj.object.ContentType})
	sort.Slice(pins, func(i, j int) bool { return pins[i].Role < pins[j].Role })
	return canonical.Marshal(pins)
}

// Selected parser producer/Params/labels locks only. Caller must compose source,
// qualification and complete inventory authority; any error requires rollback.
func (s SQLTransferSourceReader) lockFaceParseEvidenceProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof faceParsePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validFaceParsePreparationProofScope(s, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	pins, err := marshalFaceParseEvidencePins(proof)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	p := proof.evidence
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, p.objects[0][0].ref.ArtifactID, p.objects[0][0].ref.SHA256, p.objects[1][0].ref.ArtifactID, p.objects[1][0].ref.SHA256, proof.pixels.ref.ArtifactID, proof.pixels.ref.SHA256, p.expected, string(pins)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

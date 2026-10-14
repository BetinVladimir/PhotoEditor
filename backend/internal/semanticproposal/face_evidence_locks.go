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

func marshalFaceEvidencePins(proof facePreparationProof) ([]byte, error) {
	reader := SQLTransferSourceReader{ProjectID: proof.project, Bucket: proof.bucket}
	if !validFacePreparationProofScope(reader, auth.Identity{UserID: proof.actor, SessionID: proof.session}, proof.tenant, proof.binding, proof) {
		return nil, v2input.ErrInvalid
	}
	roles := [2][2]string{{"face_landmarks", "landmark_params"}, {"face_boxes", "detector_params"}}
	pins := make([]manualEvidencePin, 0, 4)
	for i, pair := range proof.objects {
		for j, p := range pair {
			pins = append(pins, manualEvidencePin{roles[i][j], p.ref.ArtifactID, p.ref.SHA256, proof.bucket, p.object.Key, p.object.VersionID, p.object.Bytes, p.object.ContentType})
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Role < pins[j].Role })
	return canonical.Marshal(pins)
}

// Selected producer/Params evidence only. Caller must compose qualified source,
// policy locks and complete inventory barrier/recheck; errors require rollback.
func (s SQLTransferSourceReader) lockFaceEvidenceProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof facePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validFacePreparationProofScope(s, who, tenantID, binding, proof) {
		return v2input.ErrInvalid
	}
	pins, err := marshalFaceEvidencePins(proof)
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
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.objects[0][0].ref.ArtifactID, proof.objects[0][0].ref.SHA256, proof.objects[1][0].ref.ArtifactID, proof.objects[1][0].ref.SHA256, proof.expected, string(pins)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetDefectTransports struct{ person, landmarks, skin, parser, detector v2input.Built }
type nativeTargetDefectDependencies struct {
	preparation             nativeTargetDefectPreparation
	transports              nativeTargetDefectTransports
	person, landmarks, skin string
	seal                    string
}

// All physical reads finish before the caller starts the consumer transaction.
// Each complete admission packet retains original parent/request authority.
func (s SQLTransferSourceReader) prepareNativeTargetDefectDependencies(ctx context.Context, who auth.Identity, original detectorDraftPlan, p nativeTargetDefectPreparation, color TransferColorProfile, transports nativeTargetDefectTransports) (nativeTargetDefectDependencies, error) {
	var zero nativeTargetDefectDependencies
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	person, err := s.marshalNativeTargetPersonAdmissionEvidence(ctx, who, original, p.personPlan, transports.person)
	if err != nil {
		return zero, err
	}
	landmarks, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, p.landmarksPlan, transports.landmarks, transports.detector)
	if err != nil {
		return zero, err
	}
	skin, err := s.marshalNativeTargetSkinCompletedEvidence(ctx, who, original, p.skinPlan, color, p.skin, transports.skin, transports.parser, transports.detector)
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativeTargetDefectPreparation(ctx, who, p, color); err != nil {
		return zero, err
	}
	proof := nativeTargetDefectDependencies{preparation: p, transports: transports, person: string(person), landmarks: string(landmarks), skin: string(skin)}
	proof.seal, err = targetDefectDependencyDigest(proof)
	if err != nil {
		return zero, err
	}
	return proof, ctx.Err()
}

func targetDefectDependencyDigest(p nativeTargetDefectDependencies) (string, error) {
	if err := verifyNativeTargetDefectPreparation(context.Background(), p.preparation); err != nil {
		return "", err
	}
	for _, packet := range []string{p.person, p.landmarks, p.skin} {
		if len(packet) < 2 || len(packet) > 32<<20 || !json.Valid([]byte(packet)) {
			return "", v2input.ErrInvalid
		}
	}
	return canonical.Hash(map[string]any{"preparation": p.preparation.seal, "person": p.person, "landmarks": p.landmarks, "skin": p.skin, "transports": map[string]any{"person": p.transports.person, "landmarks": p.transports.landmarks, "skin": p.transports.skin, "parser": p.transports.parser, "detector": p.transports.detector}})
}

// Caller holds the original parent lock and rolls back any error. This is a
// canonical and completed-run barrier; defect qualification must additionally
// execute in the future blocked-begin/admission transaction.
func (s SQLTransferSourceReader) lockNativeTargetDefectDependenciesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, p nativeTargetDefectDependencies) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	digest, err := targetDefectDependencyDigest(p)
	if err != nil {
		return err
	}
	if p.seal == "" || digest != p.seal {
		return v2input.ErrConflict
	}
	f := p.preparation
	t := p.transports
	person, err := s.marshalNativeTargetPersonAdmissionEvidence(ctx, who, original, f.personPlan, t.person)
	if err != nil {
		return err
	}
	landmarks, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, f.landmarksPlan, t.landmarks, t.detector)
	if err != nil {
		return err
	}
	if string(person) != p.person || string(landmarks) != p.landmarks {
		return v2input.ErrConflict
	}
	if err := s.lockNativeTargetPersonResult(ctx, tx, who, original, f.personPlan, t.person, f.person); err != nil {
		return err
	}
	if err := s.lockNativeTargetFaceResult(ctx, tx, who, original, f.landmarksPlan, t.landmarks, t.detector, f.landmarks); err != nil {
		return err
	}
	if err := s.lockNativeCompletedTargetSkinInTransaction(ctx, tx, who, f.skinPlan, json.RawMessage(p.skin)); err != nil {
		return err
	}
	producers, err := canonical.Marshal([]map[string]any{
		{"operation": "mask.person", "run_id": f.personPlan.person.shell.Run, "task_id": f.personPlan.person.shell.Tasks[0].ID, "result_id": f.person.result},
		{"operation": "face.landmarks", "run_id": f.landmarksPlan.shell.Run, "task_id": f.landmarksPlan.shell.Tasks[0].ID, "result_id": f.landmarks.catalog.ResultID},
		{"operation": "mask.face_skin", "run_id": f.skinPlan.shell.Run, "task_id": f.skinPlan.shell.Tasks[0].ID, "result_id": f.skin.catalog.ResultID},
	})
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_defect_completed_dependencies($1,$2,$3,$4,$5,$6::jsonb)", original.request.TenantID, s.ProjectID, who.UserID, who.SessionID, f.personPlan.anchor.entry.Namespace, string(producers)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

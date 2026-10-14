package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Byte verification precedes SQL; this packet delegates no admission authority.
func (s SQLTransferSourceReader) marshalNativeLandmarkCompletedEvidence(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan, color TransferColorProfile, completed completedNativeLandmarks, built, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	current, err := s.readCompletedNativeLandmarks(ctx, who, plan, color)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, completed) {
		return nil, v2input.ErrConflict
	}
	admission, err := s.marshalNativeFaceAdmissionEvidence(ctx, who, plan, current.detector, built, detectorBuilt)
	if err != nil {
		return nil, err
	}
	files := make([]nativeSkinCompletedFile, 2)
	for i, entry := range current.objects {
		files[i] = nativeSkinCompletedFile{Role: []string{"face_landmarks", "params"}[i], Ref: entry.ref, Bucket: s.Bucket, Key: entry.object.Key, Version: entry.object.VersionID, Bytes: entry.object.Bytes, Media: entry.object.ContentType}
	}
	packet, err := canonical.Marshal(map[string]any{"schema_version": 1, "admission": admission, "result_id": current.catalog.ResultID, "outputs": current.catalog.Outputs, "params": current.catalog.Params, "files": files})
	if err != nil {
		return nil, err
	}
	if len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}

// SQL-only exact dependency lock. Caller rolls back every error and composes
// skin/source/qualification/Params checks before admitting the defect task.
func (s SQLTransferSourceReader) lockNativeCompletedLandmarksInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeFaceDraftPlan, packet json.RawMessage) error {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return err
	}
	if tx == nil || len(packet) < 2 || len(packet) > 32<<20 || plan.prepared.operation != "face.landmarks" {
		return v2input.ErrInvalid
	}
	owned, err := s.compileNativeFaceDraftPlan(ctx, who, plan.detector, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return err
	}
	if owned.seal != plan.seal {
		return v2input.ErrConflict
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
		return err
	}
	r := plan.detector.request
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_landmark_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, plan.shell.Tasks[0].ID, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeDetectorResultLockPin struct {
	Role       string    `json:"role"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA        string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	Version    string    `json:"version"`
	Bytes      int64     `json:"bytes"`
	Media      string    `json:"media_type"`
}
type nativeDetectorResultLockEvidence struct {
	SchemaVersion int                            `json:"schema_version"`
	ResultID      uuid.UUID                      `json:"result_id"`
	Output        v2input.Ref                    `json:"output"`
	Params        v2input.Ref                    `json:"params"`
	Files         [2]nativeDetectorResultLockPin `json:"files"`
}

// Caller must commit only after full downstream admission and roll back every
// failure. No object IO occurs here; retained original bytes were checked before.
func (s SQLTransferSourceReader) lockCompletedDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan, completed completedNativeDetector) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	raw, err := s.marshalCompletedDetectorLockEvidence(ctx, who, plan, completed)
	if err != nil {
		return err
	}
	task, err := plan.prepared.frozen.DraftTask()
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.request.TenantID.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_detector_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", plan.request.TenantID, s.ProjectID, who.UserID, who.SessionID, plan.request.RunID, plan.request.RequestSHA, task.ID, string(raw)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Freeze bounded byte-verified completed dependency evidence before opening SQL.
func (s SQLTransferSourceReader) marshalCompletedDetectorLockEvidence(ctx context.Context, who auth.Identity, plan detectorDraftPlan, completed completedNativeDetector) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	if _, err := s.recompileDetectorDraftPlan(ctx, who, plan); err != nil {
		return nil, err
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if completed.catalog.ResultID == uuid.Nil || completed.catalog.Params != (v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}) || completed.objects[0].ref != completed.catalog.Output || completed.objects[1].ref != completed.catalog.Params || !validFacePreparationObjects(completed.objects, [2]string{s.Bucket, s.Bucket}, s.Bucket, plan.request.Intent.Reference.Source) {
		return nil, v2input.ErrInvalid
	}
	bodies := []string{completed.body, completed.detectorParamsBody, completed.sourceBody}
	refs := []v2input.Ref{completed.catalog.Output, completed.catalog.Params, plan.request.Intent.Reference.Source}
	for i, body := range bodies {
		if len(body) < 2 || len(body) > 1<<20 {
			return nil, v2input.ErrInvalid
		}
		hash := sha256.Sum256([]byte(body))
		if hex.EncodeToString(hash[:]) != refs[i].SHA256 {
			return nil, v2input.ErrInvalid
		}
	}
	evidence := nativeDetectorResultLockEvidence{SchemaVersion: 1, ResultID: completed.catalog.ResultID, Output: completed.catalog.Output, Params: completed.catalog.Params}
	// SQL orders the exact object pins by role: detector_params before face_boxes.
	for i, index := range []int{1, 0} {
		entry := completed.objects[index]
		role := "face_boxes"
		if index == 1 {
			role = "detector_params"
		}
		if entry.object.ContentType != "application/json" || entry.object.Bytes != int64(len(bodies[index])) {
			return nil, v2input.ErrInvalid
		}
		evidence.Files[i] = nativeDetectorResultLockPin{role, entry.ref.ArtifactID, entry.ref.SHA256, s.Bucket, entry.object.Key, entry.object.VersionID, entry.object.Bytes, entry.object.ContentType}
	}
	raw, err := canonical.Marshal(evidence)
	if err != nil || len(raw) > 16384 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

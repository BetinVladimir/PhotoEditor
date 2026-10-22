package semanticreview

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strconv"
)

// LockShownExposureParams repeats canonical selection authority in the caller's
// Apply transaction. It neither approves the edit nor admits the child. Use only
// for a new mutation after idempotency replay, retaining locks until commit.
func LockShownExposureParams(ctx context.Context, tx pgx.Tx, who auth.Identity,
	preview ExposurePreviewParams, compiled CompiledShownExposureAdoption) (LockedPreviewSelection, error) {
	empty := LockedPreviewSelection{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	p := compiled.params
	_, previewSHA, err := FreezeExposurePreviewParams(preview)
	if err != nil || tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		p.TenantID != preview.TenantID || p.TargetID != preview.TargetID ||
		p.ModelLockSHA != preview.ModelLockSHA || p.RuntimeLockSHA != preview.RuntimeLockSHA || p.DeviceProfileSHA != preview.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	body, digest, err := FreezeShownExposureAdoptionParams(p)
	if err != nil || digest != compiled.ParamsRef.SHA256 || !bytes.Equal(body, compiled.body) ||
		compiled.TaskID != uuid.NewSHA1(p.ChildRunID, []byte("semantic.adopt_shown_exposure:"+p.TargetID.String())) ||
		compiled.ParamsRef.ArtifactID != uuid.NewSHA1(compiled.TaskID, []byte("Params")) {
		return empty, ErrInvalid
	}
	revision, err := strconv.ParseInt(preview.WorkflowRevision, 10, 64)
	if err != nil {
		return empty, ErrInvalid
	}
	targetRevision, err := strconv.ParseInt(preview.TargetRevision, 10, 64)
	if err != nil {
		return empty, ErrInvalid
	}
	approved, err := canonical.Marshal(p.ApprovedEdit)
	if err != nil {
		return empty, ErrInvalid
	}
	var run uuid.UUID
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT preview_id,receipt FROM photo.lock_semantic_shown_exposure_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",
		p.TenantID, preview.IntentID, who.UserID, who.SessionID, p.ChildRunID, p.TargetID, revision, targetRevision,
		preview.Plan.SHA256, preview.Candidate.SHA256, string(body), digest, string(approved)).Scan(&run, &raw)
	if err != nil {
		return empty, err
	}
	expected := ExposurePreviewReceipt{SchemaVersion: 1, ParamsSHA: previewSHA, TaskID: p.PreviewTaskID, ResultID: p.PreviewResultID,
		Attempt: p.PreviewAttempt, Fence: p.PreviewFence, Presentation: p.Presentation, WorkingImage: p.WorkingImage, Pixels: p.Pixels, NumericalEvidence: p.NumericalEvidence}
	var receipt ExposurePreviewReceipt
	if len(raw) < 2 || len(raw) > 16384 || decodeStrict(raw, &receipt) != nil || run != p.PreviewRunID || receipt != expected {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return LockedPreviewSelection{TargetID: p.TargetID, PreviewRunID: run, Receipt: receipt}, nil
}

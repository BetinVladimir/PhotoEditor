package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PreviewAdmission retains the stable recovery identity even when commit returns
// an error. CommitAttempted means outcome must be read, not presumed rolled back.
type PreviewAdmission struct {
	RunID           uuid.UUID
	TaskID          uuid.UUID
	CommitAttempted bool
	RunStatus       string
	Recovered       bool
}

// AdmitExposurePreviewRun joins preparation/publication and atomic SQL activation.
// Input is server-resolved; transform bytes must be the candidate's pinned
// transform document. SQL independently checks its producer and exact hash.
func AdmitExposurePreviewRun(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects,
	bucket string, who auth.Identity, input ExposurePreviewPreparation, transform []byte) (PreviewAdmission, error) {
	empty := PreviewAdmission{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(transform) < 2 || len(transform) > 1048576 {
		return empty, ErrInvalid
	}
	transform = append([]byte(nil), transform...)
	owned, err := PrepareReviewDocuments(input.Review.Sources, input.Review.documents, input.Review.modelSHA, input.Review.datasetSHA)
	if err != nil {
		return empty, err
	}
	input.Review = owned
	input.Source.Documents = ExposureSourceDocuments{
		Source:     append([]byte(nil), input.Source.Documents.Source...),
		Reference:  append([]byte(nil), input.Source.Documents.Reference...),
		Mask:       append([]byte(nil), input.Source.Documents.Mask...),
		Protection: append([]byte(nil), input.Source.Documents.Protection...)}
	params, err := CompileExposurePreviewParams(input.Tenant, input.Intent, input.Workflow, owned, input.Target,
		input.Request, input.Source.Protection, input.Source.Qualification)
	if err != nil {
		return empty, err
	}
	var candidate, intent []byte
	for _, doc := range owned.documents {
		if doc.Source.Ref == params.Candidate {
			candidate = append([]byte(nil), doc.Body...)
		}
		if doc.Source.Ref == params.Intent {
			intent = append([]byte(nil), doc.Body...)
		}
	}
	decoded, err := v2input.DecodeSemanticCandidate(candidate)
	sum := sha256.Sum256(transform)
	if err != nil || decoded.Transform == nil || decoded.Transform.SHA256 != hex.EncodeToString(sum[:]) {
		return empty, ErrInvalid
	}
	_, width, height, err := ResolveExposurePreviewDocuments(params, candidate, intent, input.Source.Documents)
	if err != nil {
		return empty, err
	}
	if _, err := CompileExposurePreviewResources(width, height, input.Source.OwnerCosts, input.Source.Limits); err != nil {
		return empty, err
	}
	run, err := SaveExposurePreview(ctx, db, who, params)
	if err != nil {
		return empty, err
	}
	result := PreviewAdmission{RunID: run}
	inspection, err := InspectExposurePreviewRun(ctx, db, who, run, params)
	if err != nil {
		return result, err
	}
	recovered, resume, err := classifyPreviewRecovery(inspection)
	if err != nil {
		return result, err
	}
	if !resume {
		return recovered, nil
	}
	prepared, err := PrepareExposurePreviewRun(ctx, db, objects, bucket, who, input)
	if err != nil {
		return result, err
	}
	if prepared.RunID != run {
		return result, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.CommitAttempted = true
	result.TaskID, err = CommitExposurePreviewRun(ctx, db, who, prepared.RunID, params, prepared.Commit,
		candidate, intent, transform, input.Source.Documents)
	if err == nil {
		result.RunStatus = "queued"
	}
	return result, err
}

// Only an inspected empty draft may resume publication. Every sealed result is
// reused, including failure/cancellation; the same request is never restarted.
func classifyPreviewRecovery(state PreviewInspection) (PreviewAdmission, bool, error) {
	result := PreviewAdmission{RunID: state.RunID, TaskID: state.TaskID, RunStatus: state.RunStatus, Recovered: true}
	if state.RunID == uuid.Nil || state.TaskID == uuid.Nil || state.TaskPresent != (state.TaskState != "") {
		return PreviewAdmission{}, false, ErrInvalid
	}
	if state.RunStatus == "draft" && !state.GraphSealed && !state.TaskPresent {
		return result, true, nil
	}
	if !state.GraphSealed || !state.TaskPresent {
		return result, false, ErrConflict
	}
	switch state.RunStatus {
	case "queued", "running", "completed", "failed", "cancelled", "cancelling":
		return result, false, nil
	default:
		return result, false, ErrInvalid
	}
}

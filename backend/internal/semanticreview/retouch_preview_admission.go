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
// AdmitRetouchPreviewRun joins preparation/publication and atomic SQL activation.
// Input is server-resolved; transform bytes must be the candidate's pinned
// transform document. SQL independently checks its producer and exact hash.
func AdmitRetouchPreviewRun(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects,
	bucket string, who auth.Identity, input RetouchPreviewPreparation, transform []byte) (PreviewAdmission, error) {
	empty := PreviewAdmission{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || input.Source.TargetID != input.Target.ID || len(transform) < 2 || len(transform) > 1048576 {
		return empty, ErrInvalid
	}
	transform = append([]byte(nil), transform...)
	owned, err := PrepareReviewDocuments(input.Review.Sources, input.Review.documents, input.Review.modelSHA, input.Review.datasetSHA)
	if err != nil {
		return empty, err
	}
	input.Review = owned
	input.ContextBody = append([]byte(nil), input.ContextBody...)
	input.Source.Documents = ExposureSourceDocuments{
		Source:     append([]byte(nil), input.Source.Documents.Source...),
		Reference:  append([]byte(nil), input.Source.Documents.Reference...),
		Mask:       append([]byte(nil), input.Source.Documents.Mask...),
		Protection: append([]byte(nil), input.Source.Documents.Protection...)}
	params, err := CompileRetouchPreviewParams(input.Tenant, input.Intent, input.Workflow, owned, input.Target,
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
	_, width, height, err := ResolveRetouchPreviewDocuments(params, candidate, intent, input.Source.Documents)
	if err != nil {
		return empty, err
	}
	if _, err := CompileRetouchPreviewResources(width, height, input.Source.OwnerCosts, input.Source.Limits); err != nil {
		return empty, err
	}
	if _, _, err := BindRetouchPreviewContext(params, input.ContextRef, input.ContextBody, input.Source.Documents.Mask); err != nil {
		return empty, err
	}
	run, err := SaveRetouchPreview(ctx, db, who, params, input.ContextRef, input.ContextBody, input.Source.Documents.Mask)
	if err != nil {
		return empty, err
	}
	result := PreviewAdmission{RunID: run}
	inspection, err := InspectRetouchPreviewRun(ctx, db, who, run, params, input.ContextRef)
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
	prepared, err := PrepareRetouchPreviewRun(ctx, db, objects, bucket, who, input)
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
	result.TaskID, err = CommitRetouchPreviewRun(ctx, db, who, prepared.RunID, params, prepared.Commit,
		candidate, intent, transform, input.Source.Documents)
	if err == nil {
		result.RunStatus = "queued"
	}
	return result, err
}

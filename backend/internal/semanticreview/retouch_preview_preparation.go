package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RetouchPreviewPreparation consists of server-resolved review rows and pinned
// source documents, not a client request DTO. Every producer/ACL fence must be
// live; publication and quota repeat SQL authority before changing state.
type RetouchPreviewPreparation struct {
	ContextRef               v2input.Ref
	ContextBody              []byte
	Tenant, Intent, Workflow uuid.UUID
	Review                   PreparedReview
	Target                   LockedTarget
	Request                  PreviewRequest
	Source                   RetouchTargetDocuments
}
type RetouchTargetDocuments struct {
	TargetID      uuid.UUID
	Protection    v2input.Ref
	Documents     ExposureSourceDocuments
	OwnerCosts    ExposureOwnerCosts
	Limits        ExposureResourceEstimate
	Qualification RetouchQualificationPin
}

// PrepareRetouchPreviewRun authors and publishes one independent preview task.
// A successful result remains non-dispatchable until complete relational input
// registration, materialization and sealing. Failed IO leaves durable replayable
// tickets; abandoned drafts are handled by stale cancellation and cleanup.
func PrepareRetouchPreviewRun(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects,
	bucket string, who auth.Identity, input RetouchPreviewPreparation) (PreparedPreviewRun, error) {
	empty := PreparedPreviewRun{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || who.UserID == uuid.Nil ||
		who.SessionID == uuid.Nil || input.Source.TargetID != input.Target.ID {
		return empty, ErrInvalid
	}
	params, err := CompileRetouchPreviewParams(input.Tenant, input.Intent, input.Workflow, input.Review,
		input.Target, input.Request, input.Source.Protection, input.Source.Qualification)
	if err != nil {
		return empty, err
	}
	owned, err := PrepareReviewDocuments(input.Review.Sources, input.Review.documents, input.Review.modelSHA, input.Review.datasetSHA)
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
	// Own external source bytes before the first database wait.
	docs := ExposureSourceDocuments{Source: append([]byte(nil), input.Source.Documents.Source...),
		Reference: append([]byte(nil), input.Source.Documents.Reference...), Mask: append([]byte(nil), input.Source.Documents.Mask...),
		Protection: append([]byte(nil), input.Source.Documents.Protection...)}
	_, width, height, err := ResolveRetouchPreviewDocuments(params, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	if _, err := CompileRetouchPreviewResources(width, height, input.Source.OwnerCosts, input.Source.Limits); err != nil {
		return empty, err
	}
	contextBody := append([]byte(nil), input.ContextBody...)
	if _, _, err := BindRetouchPreviewContext(params, input.ContextRef, contextBody, docs.Mask); err != nil {
		return empty, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, input.Tenant, input.Intent, input.Target.ID, input.Request); err != nil {
		return empty, err
	}
	run, err := SaveRetouchPreview(ctx, db, who, params, input.ContextRef, contextBody, docs.Mask)
	if err != nil {
		return empty, err
	}
	frozen, err := CompileRetouchPreviewTask(run, params, candidate, intent, docs, input.Source.OwnerCosts, input.Source.Limits, input.Source.Qualification, input.ContextRef, contextBody)
	if err != nil {
		return empty, err
	}
	accepted, err := ReserveRetouchPreviewQuota(ctx, db, who, run, frozen, docs.Source)
	if err != nil {
		return empty, err
	}
	if !accepted {
		return empty, ErrConflict
	}
	publisher, err := NewRetouchPreviewPublisher(db, objects, bucket, who.UserID, who.SessionID, run, frozen, contextBody, docs.Mask)
	if err != nil {
		return empty, err
	}
	if _, err := PublishRetouchPreviewContext(ctx, frozen, contextBody, publisher); err != nil {
		return empty, err
	}
	task, err := PublishRetouchPreviewTask(ctx, frozen, publisher)
	if err != nil {
		return empty, err
	}
	task, err = VerifyRetouchPreviewPublication(ctx, frozen, task)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, input.Tenant, input.Intent, input.Target.ID, input.Request); err != nil {
		return empty, err
	}
	commit, err := PrepareRetouchPreviewCommitInputs(ctx, frozen, task)
	if err != nil {
		return empty, err
	}
	return PreparedPreviewRun{RunID: run, Task: task, Commit: commit}, nil
}

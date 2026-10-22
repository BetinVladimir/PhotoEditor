package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ShownPreparationInput contains already physically verified canonical preview
// and trusted deployment costs. Live authority must be repeated by recheck.
type ShownPreparationInput struct {
	Selected          LockedPreviewSelection
	Physical          PhysicalExposurePreview
	Candidate, Intent []byte
	Documents         ExposureSourceDocuments
	Owner             ExposureOwnerCosts
	Limits            ExposureResourceEstimate
	Qualification     ExposureQualificationPin
}

// PrepareShownApplyBatch publishes approved edits, exact copy task closures and
// decision. It creates no run/task and must be followed by atomic Apply commit.
func PrepareShownApplyBatch(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	who auth.Identity, tenant, intent, child uuid.UUID, review PreparedReview, targets []LockedTarget,
	request ApplyDecision, inputs []ShownPreparationInput, recheck func(context.Context) error) (PreparedApply, error) {
	empty := PreparedApply{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || objects == nil || recheck == nil {
		return empty, ErrInvalid
	}
	publication := exposureBatchPublication{
		approved: func(ctx context.Context, decision ApplyDecision) error {
			_, err := PublishApprovedTaskParams(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, child, review, targets, decision)
			return err
		},
		copyPublisher: func(input ShownPreparationInput, frozen FrozenShownExposureTask) (v2input.Publisher, error) {
			return NewShownExposurePublisher(db, objects, bucket, who.UserID, who.SessionID, review.Sources.RunID, input.Physical.linkage.Presentation.Params, frozen)
		},
		decision: func(ctx context.Context, decision ApplyDecision) (PublishedDecision, error) {
			return PublishDecision(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, review, targets, decision)
		},
	}
	return prepareShownExposureBatch(ctx, tenant, intent, child, review, targets, request, inputs, recheck, publication)
}

// Private publication boundary keeps the real compiler and orchestration in
// unit scenarios without requiring database or object-storage side effects.
type exposureBatchPublication struct {
	approved func(context.Context, ApplyDecision) error
	copyPublisher func(ShownPreparationInput, FrozenShownExposureTask) (v2input.Publisher, error)
	decision func(context.Context, ApplyDecision) (PublishedDecision, error)
}

func prepareShownExposureBatch(ctx context.Context, tenant, intent, child uuid.UUID,
	review PreparedReview, targets []LockedTarget, request ApplyDecision, inputs []ShownPreparationInput,
	recheck func(context.Context) error, publication exposureBatchPublication) (PreparedApply, error) {
	empty := PreparedApply{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if recheck == nil || publication.approved == nil || publication.copyPublisher == nil || publication.decision == nil {
		return empty, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return empty, err
	}
	drafts, err := CompileApprovedTaskDrafts(child, review, targets, owned)
	if err != nil {
		return empty, err
	}
	if len(drafts) == 0 || len(inputs) != len(drafts) {
		return empty, ErrInvalid
	}
	byTarget := map[uuid.UUID]ShownPreparationInput{}
	decisions := map[uuid.UUID]TargetDecision{}
	for _, decision := range owned.TargetDecisions {
		decisions[decision.TargetID] = decision
	}
	// Validate every closure before the first publication, including duplicate targets.
	for _, input := range inputs {
		id := input.Selected.TargetID
		if _, exists := byTarget[id]; exists {
			return empty, ErrInvalid
		}
		// Keep verified raw preview documents owned across authority callbacks.
		// Copying PhysicalExposurePreview alone retains shared byte slices.
		input.Physical.image.ImageBytes = append([]byte(nil), input.Physical.image.ImageBytes...)
		input.Physical.linkage.Document.raw = append([]byte(nil), input.Physical.linkage.Document.raw...)
		input.Physical.linkage.Evidence.raw = append([]byte(nil), input.Physical.linkage.Evidence.raw...)
		input.Candidate = append([]byte(nil), input.Candidate...)
		input.Intent = append([]byte(nil), input.Intent...)
		input.Documents.Source = append([]byte(nil), input.Documents.Source...)
		input.Documents.Reference = append([]byte(nil), input.Documents.Reference...)
		input.Documents.Mask = append([]byte(nil), input.Documents.Mask...)
		input.Documents.Protection = append([]byte(nil), input.Documents.Protection...)
		byTarget[id] = input
	}
	for _, draft := range drafts {
		input, ok := byTarget[draft.TargetID]
		decision := decisions[draft.TargetID]
		if !ok || decision.Preview == nil || decision.Decision != "approve" ||
			decision.Preview.Render.ArtifactID != input.Selected.Receipt.WorkingImage.ArtifactID ||
			decision.Preview.Render.SHA256 != input.Selected.Receipt.WorkingImage.SHA256 ||
			decision.Preview.Evidence.ArtifactID != input.Selected.Receipt.Presentation.ArtifactID ||
			decision.Preview.Evidence.SHA256 != input.Selected.Receipt.Presentation.SHA256 {
			return empty, ErrInvalid
		}
		compiled, e := CompileShownExposureAdoption(child, draft, input.Selected, input.Physical)
		if e != nil {
			return empty, e
		}
		if _, e = CompileShownExposureTask(compiled, draft, input.Physical, input.Candidate, input.Intent, input.Documents, input.Owner, input.Limits, input.Qualification); e != nil {
			return empty, e
		}
		if _, e = shownExposureDocumentBundle(draft, input.Physical, input.Candidate, input.Intent, input.Documents); e != nil {
			return empty, e
		}
	}
	if err = recheckShownExposurePublication(ctx, recheck); err != nil {
		return empty, err
	}
	if err = publication.publishApproved(ctx, raw); err != nil {
		return empty, err
	}
	result := PreparedApply{Review: review, Targets: append([]LockedTarget(nil), targets...), Child: child}
	for _, draft := range drafts {
		input := byTarget[draft.TargetID]
		task, e := PrepareShownExposureWithPublisher(ctx, child, draft, input.Selected, input.Physical, input.Candidate, input.Intent,
			input.Documents, input.Owner, input.Limits, input.Qualification, func(frozen FrozenShownExposureTask) (v2input.Publisher, error) {
				return publication.copyPublisher(input, frozen)
			}, func(checkCtx context.Context) error {
				return recheckShownExposurePublication(checkCtx, recheck)
			})
		if e != nil {
			return empty, e
		}
		result.ShownTasks = append(result.ShownTasks, task)
	}
	if _, err = VerifyShownExposureBatch(tenant, intent, child, review, targets, owned, result.ShownTasks); err != nil {
		return empty, err
	}
	if err = recheckShownExposurePublication(ctx, recheck); err != nil {
		return empty, err
	}
	result.Decision, err = publication.publishDecision(ctx, raw)
	if err != nil {
		return empty, err
	}
	if err = recheckShownExposurePublication(ctx, recheck); err != nil {
		return empty, err
	}
	return result, nil
}

// Authority callbacks can cancel the context while returning nil. Fence both
// sides so no subsequent publication phase starts after cancellation.
func recheckShownExposurePublication(ctx context.Context, recheck func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if recheck == nil {
		return ErrInvalid
	}
	err := recheck(ctx)
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	return err
}

// Each publisher receives a separate deep decode of the frozen request.
func (p exposureBatchPublication) publishApproved(ctx context.Context, raw []byte) error {
	if p.approved == nil {
		return ErrInvalid
	}
	request, err := DecodeApplyDecision(raw)
	if err != nil {
		return err
	}
	return p.approved(ctx, request)
}

func (p exposureBatchPublication) publishDecision(ctx context.Context, raw []byte) (PublishedDecision, error) {
	if p.decision == nil {
		return PublishedDecision{}, ErrInvalid
	}
	request, err := DecodeApplyDecision(raw)
	if err != nil {
		return PublishedDecision{}, err
	}
	return p.decision(ctx, request)
}

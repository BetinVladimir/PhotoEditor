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

// ShownRemovalPreparationInput contains already physically verified canonical preview
// and trusted deployment costs. Live authority must be repeated by recheck.
type ShownRemovalPreparationInput struct {
	Selected          LockedRemovalPreviewSelection
	Physical          PhysicalRemovalPreview
	Candidate, Intent []byte
	Documents         ExposureSourceDocuments
	Owner             ExposureOwnerCosts
	Limits            ExposureResourceEstimate
	Qualification     ExposureQualificationPin
}

// PrepareShownRemovalApplyBatch publishes approved edits, exact copy task closures and
// decision. It creates no run/task and must be followed by atomic Apply commit.
func PrepareShownRemovalApplyBatch(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	who auth.Identity, tenant, intent, child uuid.UUID, review PreparedReview, targets []LockedTarget,
	request ApplyDecision, inputs []ShownRemovalPreparationInput, recheck func(context.Context) error) (PreparedRemovalApply, error) {
	if db == nil || objects == nil {
		if err := ctx.Err(); err != nil { return PreparedRemovalApply{}, err }
		return PreparedRemovalApply{}, ErrInvalid
	}
	publication := removalBatchPublication{
		approved: func(ctx context.Context, owned ApplyDecision) error {
			_, err := PublishApprovedTaskParams(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, child, review, targets, owned)
			return err
		},
		copyPublisher: func(input ShownRemovalPreparationInput, frozen FrozenShownRemovalTask) (v2input.Publisher, error) {
			return NewShownRemovalPublisher(db, objects, bucket, who.UserID, who.SessionID, review.Sources.RunID, input.Physical.linkage.Presentation.Params, frozen)
		},
		decision: func(ctx context.Context, owned ApplyDecision) (PublishedDecision, error) {
			return PublishDecision(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, review, targets, owned)
		},
	}
	return prepareShownRemovalBatch(ctx, tenant, intent, child, review, targets, request, inputs, recheck, publication)
}

// Private seams keep the real compiler and ordering under authored scenarios.
// The public entry binds every callback to authenticated durable publication.
type removalBatchPublication struct {
	approved func(context.Context, ApplyDecision) error
	copyPublisher func(ShownRemovalPreparationInput, FrozenShownRemovalTask) (v2input.Publisher, error)
	decision func(context.Context, ApplyDecision) (PublishedDecision, error)
}

func prepareShownRemovalBatch(ctx context.Context, tenant, intent, child uuid.UUID,
	review PreparedReview, targets []LockedTarget, request ApplyDecision, inputs []ShownRemovalPreparationInput,
	recheck func(context.Context) error, publication removalBatchPublication) (PreparedRemovalApply, error) {
	empty := PreparedRemovalApply{}
	if err := ctx.Err(); err != nil { return empty, err }
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
	byTarget := map[uuid.UUID]ShownRemovalPreparationInput{}
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
		compiled, e := CompileShownRemovalAdoption(child, draft, input.Selected, input.Physical)
		if e != nil {
			return empty, e
		}
		if _, e = CompileShownRemovalTask(compiled, draft, input.Physical, input.Candidate, input.Intent, input.Documents, input.Owner, input.Limits, input.Qualification); e != nil {
			return empty, e
		}
		if _, e = shownRemovalDocumentBundle(draft, input.Physical, input.Candidate, input.Intent, input.Documents); e != nil {
			return empty, e
		}
	}
	if err = recheckShownRemovalPublication(ctx, recheck); err != nil {
		return empty, err
	}
	if err = publication.publishApproved(ctx, raw); err != nil {
		return empty, err
	}
	result := PreparedRemovalApply{Review: review, Targets: append([]LockedTarget(nil), targets...), Child: child}
	for _, draft := range drafts {
		input := byTarget[draft.TargetID]
		task, e := PrepareShownRemovalWithPublisher(ctx, child, draft, input.Selected, input.Physical, input.Candidate, input.Intent,
			input.Documents, input.Owner, input.Limits, input.Qualification, func(frozen FrozenShownRemovalTask) (v2input.Publisher, error) {
				return publication.copyPublisher(input, frozen)
			}, recheck)
		if e != nil {
			return empty, e
		}
		result.ShownTasks = append(result.ShownTasks, task)
	}
	if _, err = VerifyShownRemovalBatch(tenant, intent, child, review, targets, owned, result.ShownTasks); err != nil {
		return empty, err
	}
	if err = recheckShownRemovalPublication(ctx, recheck); err != nil {
		return empty, err
	}
	result.Decision, err = publication.publishDecision(ctx, raw)
	if err != nil {
		return empty, err
	}
	if err = recheckShownRemovalPublication(ctx, recheck); err != nil {
		return empty, err
	}
	return result, nil
}

// PreparedRemovalApply is a publication-complete removal-only batch. It has
// no permission to dispatch tasks until CommitShownRemoval succeeds atomically.
type PreparedRemovalApply struct {
	Review     PreparedReview
	Targets    []LockedTarget
	Decision   PublishedDecision
	Child      uuid.UUID
	ShownTasks []PreparedShownRemoval
}

// A recheck implementation may return nil even after cancelling its context.
// Do not enter another publication phase after such an authority callback.
func recheckShownRemovalPublication(ctx context.Context, recheck func(context.Context) error) error {
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

// Decode separate owned requests for each publisher; a struct copy alone shares
// TargetDecisions and nested preview pointers with the frozen selection.
func (p removalBatchPublication) publishApproved(ctx context.Context, raw []byte) error {
	if p.approved == nil { return ErrInvalid }
	request, err := DecodeApplyDecision(raw)
	if err != nil { return err }
	return p.approved(ctx, request)
}
func (p removalBatchPublication) publishDecision(ctx context.Context, raw []byte) (PublishedDecision, error) {
	if p.decision == nil { return PublishedDecision{}, ErrInvalid }
	request, err := DecodeApplyDecision(raw)
	if err != nil { return PublishedDecision{}, err }
	return p.decision(ctx, request)
}

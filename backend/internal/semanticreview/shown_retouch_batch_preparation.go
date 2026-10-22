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

// ShownRetouchPreparationInput contains already physically verified canonical preview
// and trusted deployment costs. Live authority must be repeated by recheck.
type ShownRetouchPreparationInput struct {
	Selected          LockedRetouchPreviewSelection
	Physical          PhysicalRetouchPreview
	Candidate, Intent []byte
	Documents         ExposureSourceDocuments
	Owner             ExposureOwnerCosts
	Limits            ExposureResourceEstimate
	Qualification     RetouchQualificationPin
}

// PrepareShownRetouchApplyBatch publishes approved edits, exact copy task closures and
// decision. It creates no run/task and must be followed by atomic Apply commit.
func PrepareShownRetouchApplyBatch(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	who auth.Identity, tenant, intent, child uuid.UUID, review PreparedReview, targets []LockedTarget,
	request ApplyDecision, inputs []ShownRetouchPreparationInput, recheck func(context.Context) error) (PreparedRetouchApply, error) {
	if db == nil || objects == nil {
		if err := ctx.Err(); err != nil {
			return PreparedRetouchApply{}, err
		}
		return PreparedRetouchApply{}, ErrInvalid
	}
	publication := retouchBatchPublication{
		approved: func(ctx context.Context, owned ApplyDecision) error {
			_, err := PublishApprovedTaskParams(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, child, review, targets, owned)
			return err
		},
		copyPublisher: func(input ShownRetouchPreparationInput, frozen FrozenShownRetouchTask) (v2input.Publisher, error) {
			return NewShownRetouchPublisher(db, objects, bucket, who.UserID, who.SessionID, review.Sources.RunID, input.Physical.linkage.Presentation.Params, frozen)
		},
		decision: func(ctx context.Context, owned ApplyDecision) (PublishedDecision, error) {
			return PublishDecision(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, review, targets, owned)
		},
	}
	return prepareShownRetouchBatch(ctx, tenant, intent, child, review, targets, request, inputs, recheck, publication)
}

// Private publication seams retain the real compilation/recheck orchestration.
// Production always binds these callbacks to authenticated SQL/object publishers.
type retouchBatchPublication struct {
	approved      func(context.Context, ApplyDecision) error
	copyPublisher func(ShownRetouchPreparationInput, FrozenShownRetouchTask) (v2input.Publisher, error)
	decision      func(context.Context, ApplyDecision) (PublishedDecision, error)
}

// Give each publication callback an independent request. Passing ApplyDecision
// by value alone still shares its TargetDecisions slice and preview pointers
// with the frozen request used by later batch verification.
func (p retouchBatchPublication) publishApproved(ctx context.Context, raw []byte) error {
	if p.approved == nil {
		return ErrInvalid
	}
	request, err := DecodeApplyDecision(raw)
	if err != nil {
		return err
	}
	return p.approved(ctx, request)
}

func (p retouchBatchPublication) publishDecision(ctx context.Context, raw []byte) (PublishedDecision, error) {
	if p.decision == nil {
		return PublishedDecision{}, ErrInvalid
	}
	request, err := DecodeApplyDecision(raw)
	if err != nil {
		return PublishedDecision{}, err
	}
	return p.decision(ctx, request)
}

func prepareShownRetouchBatch(ctx context.Context, tenant, intent, child uuid.UUID,
	review PreparedReview, targets []LockedTarget, request ApplyDecision, inputs []ShownRetouchPreparationInput,
	recheck func(context.Context) error, publication retouchBatchPublication) (PreparedRetouchApply, error) {
	empty := PreparedRetouchApply{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publication.approved == nil || publication.copyPublisher == nil || publication.decision == nil || recheck == nil {
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
	byTarget := map[uuid.UUID]ShownRetouchPreparationInput{}
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
		// Freeze physical bytes together with the source documents before callbacks.
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
		compiled, e := CompileShownRetouchAdoption(child, draft, input.Selected, input.Physical)
		if e != nil {
			return empty, e
		}
		if _, e = CompileShownRetouchTask(compiled, draft, input.Physical, input.Candidate, input.Intent, input.Documents, input.Owner, input.Limits, input.Qualification); e != nil {
			return empty, e
		}
		// Validate exact publication bytes for every target before publishing any
		// approved edit. Closure validation alone does not check this raw bundle.
		if _, e = shownRetouchDocumentBundle(draft, input.Physical, input.Candidate, input.Intent, input.Documents); e != nil {
			return empty, e
		}
	}
	if err = recheck(ctx); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = publication.publishApproved(ctx, raw); err != nil {
		return empty, err
	}
	result := PreparedRetouchApply{Review: review, Targets: append([]LockedTarget(nil), targets...), Child: child}
	for _, draft := range drafts {
		input := byTarget[draft.TargetID]
		task, e := PrepareShownRetouchWithPublisher(ctx, child, draft, input.Selected, input.Physical, input.Candidate, input.Intent,
			input.Documents, input.Owner, input.Limits, input.Qualification, func(frozen FrozenShownRetouchTask) (v2input.Publisher, error) {
				return publication.copyPublisher(input, frozen)
			}, recheck)
		if e != nil {
			return empty, e
		}
		result.ShownTasks = append(result.ShownTasks, task)
	}
	if _, err = VerifyShownRetouchBatch(tenant, intent, child, review, targets, owned, result.ShownTasks); err != nil {
		return empty, err
	}
	if err = recheck(ctx); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	result.Decision, err = publication.publishDecision(ctx, raw)
	if err != nil {
		return empty, err
	}
	if err = recheck(ctx); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}

// PreparedRetouchApply is a publication-complete retouch batch. It has
// no permission to dispatch tasks until CommitShownRetouch succeeds atomically.
type PreparedRetouchApply struct {
	Review     PreparedReview
	Targets    []LockedTarget
	Decision   PublishedDecision
	Child      uuid.UUID
	ShownTasks []PreparedShownRetouch
}

// asPreparedApply retains only the shown-retouch family for the service commit.
// Verification is repeated by CommitShownRetouch inside its idempotency callback.
func (p PreparedRetouchApply) asPreparedApply() PreparedApply {
	return PreparedApply{Review: p.Review, Targets: append([]LockedTarget(nil), p.Targets...),
		Decision: p.Decision, Child: p.Child, ShownRetouchTasks: append([]PreparedShownRetouch(nil), p.ShownTasks...)}
}

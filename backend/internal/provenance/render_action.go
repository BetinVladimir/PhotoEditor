package provenance

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// The review adapter must prove an explicit accepted decision about this exact
// adopted render result and report. TransferCandidate approval is insufficient.
type SigningRenderReview struct {
	ID, Task, Result uuid.UUID
	Root, Report     v2input.Ref
	Accepted         bool
}
type SigningRenderReviewReader interface {
	ReadSigningRenderReview(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref, bool) (*SigningRenderReview, error)
}

// CompileSigningRenderAction is deliberately separate from graph resolution:
// complete causal input/output edges must still be resolved before installation.
func CompileSigningRenderAction(task CanonicalSigningTask, evidence SigningRenderEvidence, approval *SigningRenderReview) (SigningAction, error) {
	var zero SigningAction
	if normalizeCanonicalSigningTask(&task) != nil || task.Operation != "render.compose" || task.Version != "1" || !evidence.qualification.valid() || evidence.qualification.ModelLockSHA != evidence.pixels.recipe.Scalars.ModelLockSHA || !ref(evidence.pixels.root) || !ref(evidence.pixels.result.Report) || evidence.pixels.generatedPixels < 0 || evidence.pixels.changedPixels < 0 || (evidence.report.Status != "pass" && evidence.report.Status != "review") {
		return zero, ErrInvalid
	}
	taskDigest, err := canonical.Hash(task)
	if err != nil || taskDigest != evidence.taskDigest {
		return zero, ErrInvalid
	}
	roots, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil || len(roots) != 1 || roots[0] != evidence.pixels.root {
		return zero, ErrInvalid
	}
	if evidence.pixels.generatedPixels > 0 && evidence.pixels.result.GeneratedMask == nil {
		return zero, ErrInvalid
	}
	required := task.ReviewRequired || evidence.inheritedReview || evidence.report.Status == "review"
	var reviewID *uuid.UUID
	if approval != nil {
		if approval.ID == uuid.Nil || !approval.Accepted || approval.Task != task.Task || approval.Result != task.Result || approval.Root != evidence.pixels.root || approval.Report != evidence.pixels.result.Report {
			return zero, ErrInvalid
		}
		id := approval.ID
		reviewID = &id
	} else if required {
		return zero, ErrInvalid
	}
	model := evidence.qualification.ModelLockSHA
	return SigningAction{Operation: task.Operation, Version: task.Version, ParamsSHA: task.Params.SHA256, ModelLockSHA: &model, Generated: evidence.pixels.generatedPixels > 0, ReviewID: reviewID}, nil
}

// ResolveSigningRenderAction rechecks review authority after compiling the
// action. A missing review adapter is an installation error, including for a
// pass report: it must establish whether an explicit decision exists.
type SigningRenderEvidenceReader interface {
	ReadSigningRenderEvidence(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderEvidence, error)
}
type ResolvedSigningRenderOutputs struct {
	Action   SigningAction
	Children []SigningChildOutput
}

func ResolveSigningRenderAction(ctx context.Context, source SigningRenderEvidenceReader, reviews SigningRenderReviewReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningAction, error) {
	outputs, err := ResolveSigningRenderOutputs(ctx, source, reviews, who, ready, task)
	if err != nil {
		return SigningAction{}, err
	}
	return outputs.Action, nil
}
func ResolveSigningRenderOutputs(ctx context.Context, source SigningRenderEvidenceReader, reviews SigningRenderReviewReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (ResolvedSigningRenderOutputs, error) {
	var zero ResolvedSigningRenderOutputs
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if reviews == nil || source == nil {
		return zero, ErrInvalid
	}
	ownedTask, err := ownSigningProducerTask(task)
	if err != nil {
		return zero, err
	}
	task = ownedTask
	isolated, err := ownSigningProducerTask(task)
	if err != nil {
		return zero, err
	}
	evidence, err := source.ReadSigningRenderEvidence(ctx, who, ready, isolated)
	if err != nil {
		return zero, err
	}
	evidenceDigest, err := signingRenderEvidenceDigest(evidence)
	if err != nil {
		return zero, err
	}
	required := task.ReviewRequired || evidence.inheritedReview || evidence.report.Status == "review"
	readReview := func() (*SigningRenderReview, error) {
		isolated, err := ownSigningProducerTask(task)
		if err != nil {
			return nil, err
		}
		return reviews.ReadSigningRenderReview(ctx, who, ready, isolated, evidence.pixels.root, evidence.pixels.result.Report, required)
	}
	approval, err := readReview()
	if err != nil {
		return zero, err
	}
	// Own the adapter result before another call can mutate a reused pointer.
	var owned *SigningRenderReview
	if approval != nil {
		value := *approval
		owned = &value
	}
	action, err := CompileSigningRenderAction(task, evidence, owned)
	if err != nil {
		return zero, err
	}
	children, err := CompileSigningRenderChildOutputs(task, evidence)
	if err != nil {
		return zero, err
	}
	current, err := readReview()
	if err != nil {
		return zero, err
	}
	currentEvidenceDigest, err := signingRenderEvidenceDigest(evidence)
	if err != nil || currentEvidenceDigest != evidenceDigest {
		return zero, ErrInvalid
	}
	if (current == nil) != (owned == nil) || (current != nil && *current != *owned) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ResolvedSigningRenderOutputs{Action: action, Children: children}, nil
}

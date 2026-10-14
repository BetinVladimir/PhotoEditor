package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// FrozenDeliveryContinuationTask pins the next-stage descriptor/transport to
// actual validated presentation refs. It deliberately exposes no initial-shell
// admission method: appending requires dedicated adopted-producer SQL authority.
type FrozenDeliveryContinuationTask struct {
	task        FrozenDeliveryInitialTask
	predecessor uuid.UUID
	stage       int
}

func FreezeDeliveryContinuationTask(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, source []byte, results []DeliveryPresentationOutput, targetICC string, registry Registry, limits DeliveryDerivativeLimits, params v2input.Ref, resolved []v2input.Entry) (FrozenDeliveryContinuationTask, error) {
	zero := FrozenDeliveryContinuationTask{}
	if len(results) < 1 {
		return zero, v2input.ErrInvalid
	}
	continuation, err := CompileDeliveryContinuation(plan, parent, child, source, results, targetICC, registry, limits)
	if err != nil {
		return zero, err
	}
	node := continuation.Node
	image, err := review.ParseWorkingImage(continuation.SourceJCS)
	if err != nil {
		return zero, err
	}
	last := results[len(results)-1]
	current := v2input.Ref{ArtifactID: last.ImageRef.ID, SHA256: last.ImageRef.SHA256}
	roots := []v2input.Entry{
		{Ref: current, Contract: "WorkingImage"},
		{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Contract: "NpyArray"},
		{Ref: child.Recipe, Contract: "EditingRecipeV3"},
		{Ref: child.Preset.Config, Contract: "ExportPresetConfig"},
		{Ref: child.Binding.Source, Contract: "WorkingImage"},
	}
	if image.Alpha != nil {
		roots = append(roots, v2input.Entry{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Contract: "NpyArray"})
	}
	// Retain every validated stage manifest/report for replay and provenance,
	// not just the last output consumed by the next operation.
	for _, result := range results {
		roots = append(roots, v2input.Entry{Ref: v2input.Ref{ArtifactID: result.ImageRef.ID, SHA256: result.ImageRef.SHA256}, Contract: "WorkingImage"}, v2input.Entry{Ref: v2input.Ref{ArtifactID: result.ReportRef.ID, SHA256: result.ReportRef.SHA256}, Contract: "DerivativeReport"})
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return zero, err
	}
	closure, err := v2input.CompileDeliveryChildClosure(workflow, node.Operation, node.Params, params, roots, resolved)
	if err != nil {
		return zero, err
	}
	draft := v2input.DraftTask{ID: node.ID, NodeKey: node.Key, Operation: node.Operation, OperationVersion: node.Version, ResourceClass: node.ResourceClass, ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: continuation.ExecutionProfile}
	return FrozenDeliveryContinuationTask{task: FrozenDeliveryInitialTask{draft: draft, closure: closure}, predecessor: continuation.Predecessor, stage: continuation.Stage}, nil
}
func (f FrozenDeliveryContinuationTask) DraftTask() (v2input.DraftTask, error) {
	return f.task.DraftTask()
}
func (f FrozenDeliveryContinuationTask) Predecessor() uuid.UUID { return f.predecessor }
func (f FrozenDeliveryContinuationTask) Stage() int             { return f.stage }
func (f FrozenDeliveryContinuationTask) PublishInputs(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	return f.task.PublishInputs(ctx, publisher)
}

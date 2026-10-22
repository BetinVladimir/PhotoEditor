package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// CompileDeliveryInitialNode chooses the first real operation from frozen
// configs. Resize/sharpen outputs must be adopted before subsequent compilation.
// Unlike legacy download task IDs, V2 tasks use UUIDv5(workflow,operation:0).
func CompileDeliveryInitialNode(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, imageRaw []byte, targetICCSHA string, registry Registry) (CompiledDownloadNode, error) {
	node, err := CompileDeliveryPresentation(plan, parent, child, imageRaw, registry)
	if err != nil {
		return CompiledDownloadNode{}, err
	}
	var compiled CompiledDownloadNode
	if node != nil {
		compiled = *node
	} else {
		compiled, err = CompileDeliveryExport(plan, parent, child, imageRaw, targetICCSHA, registry)
		if err != nil {
			return compiled, err
		}
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return CompiledDownloadNode{}, err
	}
	compiled.ID, err = v2input.TaskID(workflow, compiled.Operation, 0)
	if err != nil {
		return CompiledDownloadNode{}, err
	}
	compiled.Key, err = v2input.NodeKey(compiled.Operation, 0)
	if err != nil {
		return CompiledDownloadNode{}, err
	}
	return compiled, nil
}

type FrozenDeliveryInitialTask struct {
	metadataConfig []byte
	draft   v2input.DraftTask
	closure v2input.CaptureClosure
}

// FreezeDeliveryInitialTask reserves the actual first-stage resources and pins
// one closure for shell creation and later transport publication. No SQL/IO here.
func FreezeDeliveryInitialTask(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, imageRaw []byte, targetICCSHA string, registry Registry, paramsRef v2input.Ref, resolved []v2input.Entry, limits DeliveryDerivativeLimits) (FrozenDeliveryInitialTask, error) {
	var zero FrozenDeliveryInitialTask
	node, err := CompileDeliveryInitialNode(plan, parent, child, imageRaw, targetICCSHA, registry)
	if err != nil {
		return zero, err
	}
	profile, err := compileDeliveryInitialProfile(node, imageRaw, limits)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	roots := []v2input.Entry{
		{Ref: child.Binding.Source, Contract: "WorkingImage"},
		{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Contract: "NpyArray"},
		{Ref: child.Recipe, Contract: "EditingRecipeV3"},
		{Ref: child.Preset.Config, Contract: "ExportPresetConfig"},
	}
	if image.Alpha != nil {
		roots = append(roots, v2input.Entry{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Contract: "NpyArray"})
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return zero, err
	}
	closure, err := v2input.CompileDeliveryChildClosure(workflow, node.Operation, node.Params, paramsRef, roots, resolved)
	if err != nil {
		return zero, err
	}
	draft := v2input.DraftTask{ID: node.ID, NodeKey: node.Key, Operation: node.Operation, OperationVersion: node.Version, ResourceClass: node.ResourceClass, ParamsArtifactID: paramsRef.ArtifactID, ParamsSHA: paramsRef.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}
	return FrozenDeliveryInitialTask{draft: draft, closure: closure}, nil
}
func (f FrozenDeliveryInitialTask) DraftTask() (v2input.DraftTask, error) {
	if f.draft.ID == uuid.Nil || f.closure.Open == nil || f.draft.InputDigest != f.closure.InputDigest {
		return v2input.DraftTask{}, v2input.ErrInvalid
	}
	draft := f.draft
	draft.ExecutionProfile = append(json.RawMessage(nil), draft.ExecutionProfile...)
	return draft, nil
}
func (f FrozenDeliveryInitialTask) PublishInputs(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	draft, err := f.DraftTask()
	if err != nil || publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, draft.ID, cursor, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: draft.ID, Built: built, Open: f.closure.Open, Direct: append([]v2input.Ref(nil), f.closure.Direct...)}, nil
}


// Shared pure admission budget check. Use before durable Params publication and
// again while freezing the exact registered input closure.
func compileDeliveryInitialProfile(node CompiledDownloadNode, imageRaw []byte, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
 if node.Operation == "export.encode" {
  return CompileDeliveryEncodeProfile(node, imageRaw, limits)
 }
 return CompileDeliveryDerivativeProfile(node, imageRaw, limits)
}

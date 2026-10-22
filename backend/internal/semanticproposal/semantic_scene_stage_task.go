package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type semanticSceneStageTask struct {
	published       semanticSceneStageParams
	frozen          v2input.FrozenSemanticAnalysisTask
	inputs, taskSHA string
	costs           v2input.SemanticQualifiedCosts
}

// Resolve every physical input through accepted-source/draft authority. The
// returned task owns its closure and resources; admission must lock the retained
// source and qualification proofs before registering this exact task.
func (s SQLTransferSourceReader) prepareSemanticSceneStageTask(ctx context.Context, who auth.Identity, request Request, published semanticSceneStageParams, catalog v2draft.ArtifactCatalog, producer TransferSceneProducer, budget nativePreparationBudget) (semanticSceneStageTask, error) {
	var zero semanticSceneStageTask
	anchor := published.anchor
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifySemanticSceneStageAnchor(ctx, who, s.ProjectID, request, anchor); err != nil {
		return zero, err
	}
	current, err := s.readSceneProducerQualification(ctx, who, request.TenantID, producer)
	if err != nil {
		return zero, err
	}
	if current != published.qualification || producer.Expected.Operation != anchor.operation || producer.Costs != budget.Costs {
		return zero, v2input.ErrConflict
	}
	image := json.RawMessage(anchor.sourceBody)
	prepared, err := v2input.PrepareSemanticAnalysisParams(ctx, anchor.operation, anchor.source, image, producer.Expected.ModelLockSHA)
	if err != nil {
		return zero, err
	}
	if string(prepared.Params) != published.body {
		return zero, v2input.ErrConflict
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(anchor.source, image)
	if err != nil {
		return zero, err
	}
	nodes := []v2draft.LayerRef{{Ref: published.ref, Contract: "Params"}}
	for _, entry := range prepared.Inputs {
		node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract}
		switch {
		case entry.Ref == anchor.source.Source:
			node.CanvasHash = anchor.source.CanvasHash
		case entry.Ref == metadata.Inputs.Pixels:
			node.Contract = "WorkingPixels"
			node.ParentRef = anchor.source.Source
		case metadata.Inputs.Alpha != nil && entry.Ref == *metadata.Inputs.Alpha:
			node.Contract = "WorkingAlpha"
			node.ParentRef = anchor.source.Source
		default:
			node.Contract = "DenseWarpField"
			node.ParentRef = anchor.source.Source
		}
		nodes = append(nodes, node)
	}
	scoped := detectorParamsCatalog{reader: s, source: catalog, binding: anchor.source, document: anchor.document, params: published.ref}
	evidence, err := v2draft.ReadInputEvidence(ctx, scoped, who, request.TenantID, nodes)
	if err != nil {
		return zero, err
	}
	sizes := make([]v2input.SemanticInputSize, len(evidence))
	entries := make([]v2input.Entry, len(evidence))
	for i, pin := range evidence {
		sizes[i] = v2input.SemanticInputSize{Ref: pin.Ref, Bytes: pin.Object.Bytes}
		contract := pin.Contract
		if contract == "WorkingPixels" || contract == "WorkingAlpha" || contract == "DenseWarpField" {
			contract = "NpyArray"
		}
		entries[i] = v2input.Entry{Ref: pin.Ref, Contract: contract}
	}
	expected := producer.Expected
	frozen, err := v2input.CompileSemanticAnalysisTaskForBackend(expected.Backend, ctx, anchor.workflow, 0, anchor.operation, anchor.source, image, published.ref, v2input.SemanticAnalysisRuntimePins{ModelLockSHA: expected.ModelLockSHA, RuntimeLockSHA: expected.RuntimeLockSHA, DeviceProfileSHA: expected.DeviceProfileSHA}, sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	if err != nil {
		return zero, err
	}
	if err := frozen.VerifyInputInventory(ctx, entries); err != nil {
		return zero, err
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	taskSHA, err := canonical.Hash(task)
	if err != nil {
		return zero, err
	}
	pins, err := canonical.Marshal(evidence)
	if err != nil || len(pins) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	after, err := v2draft.ReadInputEvidence(ctx, scoped, who, request.TenantID, nodes)
	if err != nil {
		return zero, err
	}
	afterPins, err := canonical.Marshal(after)
	if err != nil || string(afterPins) != string(pins) {
		return zero, v2input.ErrConflict
	}
	current, err = s.readSceneProducerQualification(ctx, who, request.TenantID, producer)
	if err != nil {
		return zero, err
	}
	if current != published.qualification {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return semanticSceneStageTask{published: published, frozen: frozen, inputs: string(pins), taskSHA: taskSHA, costs: producer.Costs}, nil
}

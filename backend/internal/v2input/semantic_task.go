package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
)

type SemanticTransferDependencies struct {
	ReferenceWidth, ReferenceHeight, TargetWidth, TargetHeight int64
	ReferenceGamma, TargetGamma                                bool
	QualifiedCosts                                             SemanticQualifiedCosts
	Reference, Target                                          CaptureImageInputs
	SelectedPixels                                             Ref
	SourceProtection, TargetProtection                         Ref
	SourceProtectionPixels, TargetProtectionPixels             Ref
	Geometry                                                   []Ref
}

type PreparedSemanticTask struct {
	Graph       GraphTask
	Params      Ref
	ParamsJCS   json.RawMessage
	Profile     json.RawMessage
	InputDigest string
	NodeKey     string
}

type FrozenSemanticTask struct {
	snapshotID uuid.UUID
	task       uuid.UUID
	key        string
	params     Ref
	profile    json.RawMessage
	closure    CaptureClosure
	protection SemanticProtectionPin
}

// CompileSemanticTransferTask requires already scoped, hash-verified source
// and mask child refs. Source geometry/model qualification is the resolver's
// responsibility; this freezes inventory and deterministic transport identity.
func CompileSemanticTransferTask(workflow uuid.UUID, ordinal int, params SemanticTransferParams, intent SemanticEditIntent,
	paramsRef Ref, dependencies SemanticTransferDependencies, profile json.RawMessage) (FrozenSemanticTask, error) {
	var empty FrozenSemanticTask
	owned, ownedIntent, err := BindSemanticTransferIntent(params, intent)
	if err != nil {
		return empty, err
	}
	task, err := TaskID(workflow, "semantic.transfer", ordinal)
	if err != nil {
		return empty, err
	}
	key, _ := NodeKey("semantic.transfer", ordinal)

	profileFields, ok := semanticObject(profile, nil, "backend", "renderer_version", "resource_estimate")
	if !ok {
		return empty, ErrInvalid
	}
	if _, ok := semanticObject(profileFields["resource_estimate"], nil, "ram_bytes", "vram_bytes", "scratch_bytes", "output_bytes", "pixel_count"); !ok {
		return empty, ErrInvalid
	}
	var execution sessionDescribeExecutionProfile
	if !semanticDecode(profile, &execution) || execution.Backend != "cuda" || execution.RendererVersion != "v2.2" || len(dependencies.Geometry) > 64 {
		return empty, ErrInvalid
	}
	for _, cost := range []int64{execution.ResourceEstimate.RAMBytes, execution.ResourceEstimate.VRAMBytes, execution.ResourceEstimate.ScratchBytes, execution.ResourceEstimate.OutputBytes, execution.ResourceEstimate.PixelCount} {
		if cost < 0 || cost > 9007199254740991 {
			return empty, ErrInvalid
		}
	}
	if execution.ResourceEstimate.RAMBytes == 0 || execution.ResourceEstimate.ScratchBytes == 0 || execution.ResourceEstimate.OutputBytes == 0 || execution.ResourceEstimate.OutputBytes > 825169920 || execution.ResourceEstimate.PixelCount < 128 || execution.ResourceEstimate.PixelCount > 400000000 {
		return empty, ErrInvalid
	}

	budget := execution.ResourceEstimate
	if _, err := CompileSemanticTransferProfile(dependencies.ReferenceWidth, dependencies.ReferenceHeight,
		dependencies.TargetWidth, dependencies.TargetHeight, dependencies.ReferenceGamma, dependencies.TargetGamma,
		dependencies.QualifiedCosts, budget.RAMBytes, budget.VRAMBytes, budget.ScratchBytes, budget.OutputBytes); err != nil {
		return empty, err
	}
	if budget.PixelCount != dependencies.ReferenceWidth*dependencies.ReferenceHeight+dependencies.TargetWidth*dependencies.TargetHeight {
		return empty, ErrInvalid
	}
	entries := []Entry{{paramsRef, "Params"}, {owned.Intent, "EditIntent"}, {owned.Calibration, "Calibration"},
		{owned.Reference.Source, "WorkingImage"}, {owned.Target.Source, "WorkingImage"}, {ownedIntent.ReferenceMask, "MaskRef"},
		{dependencies.SourceProtection, "MaskRef"}, {dependencies.TargetProtection, "MaskRef"},
		{dependencies.Reference.Pixels, "NpyArray"}, {dependencies.Target.Pixels, "NpyArray"}, {dependencies.SelectedPixels, "NpyArray"},
		{dependencies.SourceProtectionPixels, "NpyArray"}, {dependencies.TargetProtectionPixels, "NpyArray"}}
	for _, alpha := range []*Ref{dependencies.Reference.Alpha, dependencies.Target.Alpha} {
		if alpha != nil {
			entries = append(entries, Entry{*alpha, "NpyArray"})
		}
	}
	for _, field := range dependencies.Geometry {
		entries = append(entries, Entry{field, "NpyArray"})
	}
	closure, err := compileCaptureInventory(task, "semantic.transfer", owned, paramsRef, entries)
	if err != nil {
		return empty, err
	}
	return FrozenSemanticTask{snapshotID: ownedIntent.TargetSnapshotID, task: task, key: key, params: paramsRef, profile: append(json.RawMessage(nil), profile...), closure: closure,
		protection: SemanticProtectionPin{ordinal, paramsRef, dependencies.SourceProtection, dependencies.TargetProtection}}, nil
}

func (f FrozenSemanticTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "semantic.transfer", OperationVersion: "1", ResourceClass: "gpu",
		ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}
func (f FrozenSemanticTask) ProtectionPin() (SemanticProtectionPin, error) {
	if _, err := f.DraftTask(); err != nil {
		return SemanticProtectionPin{}, err
	}
	return f.protection, nil
}

func PublishSemanticTransferTask(ctx context.Context, frozen FrozenSemanticTask, publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if _, err := frozen.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, frozen.task, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return frozen.BindPublishedTransport(ctx, built)
}

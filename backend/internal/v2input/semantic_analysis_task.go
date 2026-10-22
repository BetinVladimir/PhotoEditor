package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type SemanticAnalysisRuntimePins struct {
	ModelLockSHA     string `json:"model_lock_sha"`
	RuntimeLockSHA   string `json:"runtime_lock_sha"`
	DeviceProfileSHA string `json:"device_profile_sha"`
}
type SemanticAnalysisPreparation struct {
	Params json.RawMessage
	Inputs []Entry
}

// Pure compilation only: canonical producer, installed model qualification,
// project authority and transaction admission remain mandatory caller gates.
func PrepareSemanticAnalysisParams(ctx context.Context, operation string, binding SessionVersionBinding, imageBody json.RawMessage, modelLock string) (SemanticAnalysisPreparation, error) {
	var zero SemanticAnalysisPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !digestPattern.MatchString(modelLock) {
		return zero, ErrInvalid
	}
	inputs, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	var params any
	switch operation {
	case "scene.parse":
		params = struct {
			Image         Ref    `json:"image"`
			ModelID       string `json:"model_id"`
			ModelLock     string `json:"model_lock_id"`
			Preprocessing string `json:"preprocessing_id"`
		}{binding.Source, "segformer_b0_ade", modelLock, "segformer_ade512_official_v1"}
	case "depth.predict":
		params = struct {
			Image         Ref    `json:"image"`
			ModelID       string `json:"model_id"`
			ModelLock     string `json:"model_lock_id"`
			Preprocessing string `json:"preprocessing_id"`
			InputSize     int    `json:"input_size"`
		}{binding.Source, "depth_anything_v2_small", modelLock, "depth_anything_v2_small_518_v1", 518}
	default:
		return zero, ErrInvalid
	}
	body, err := canonical.Marshal(params)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return SemanticAnalysisPreparation{Params: body, Inputs: inputs}, nil
}

type FrozenSemanticAnalysisTask struct {
	task                    uuid.UUID
	key, operation, profile string
	params                  Ref
	closure                 CaptureClosure
}

func CompileSemanticAnalysisTask(ctx context.Context, workflow uuid.UUID, ordinal int, operation string, binding SessionVersionBinding, imageBody json.RawMessage, params Ref, pins SemanticAnalysisRuntimePins, sizes []SemanticInputSize, costs SemanticQualifiedCosts, ram, vram, scratch, output int64) (FrozenSemanticAnalysisTask, error) {
	return CompileSemanticAnalysisTaskForBackend("cuda", ctx, workflow, ordinal, operation, binding, imageBody, params, pins, sizes, costs, ram, vram, scratch, output)
}

func CompileSemanticAnalysisTaskForBackend(backend string, ctx context.Context, workflow uuid.UUID, ordinal int, operation string, binding SessionVersionBinding, imageBody json.RawMessage, params Ref, pins SemanticAnalysisRuntimePins, sizes []SemanticInputSize, costs SemanticQualifiedCosts, ram, vram, scratch, output int64) (FrozenSemanticAnalysisTask, error) {
	var zero FrozenSemanticAnalysisTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validRef(params) || !digestPattern.MatchString(pins.ModelLockSHA) || !digestPattern.MatchString(pins.RuntimeLockSHA) || !digestPattern.MatchString(pins.DeviceProfileSHA) {
		return zero, ErrInvalid
	}
	prepared, err := PrepareSemanticAnalysisParams(ctx, operation, binding, imageBody, pins.ModelLockSHA)
	if err != nil {
		return zero, err
	}
	refs := make([]Ref, len(prepared.Inputs))
	for i, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == params.ArtifactID {
			return zero, ErrInvalid
		}
		refs[i] = entry.Ref
	}
	// These two documents are physically present, so their byte sizes cannot
	// be understated even by an otherwise complete catalog descriptor set.
	for _, size := range sizes {
		if size.Ref == params && size.Bytes != int64(len(prepared.Params)) {
			return zero, ErrInvalid
		}
		if size.Ref == binding.Source && size.Bytes != int64(len(imageBody)) {
			return zero, ErrInvalid
		}
	}
	inputBytes, err := exactSemanticInputBytes(params, refs, sizes)
	if err != nil {
		return zero, err
	}
	source, err := ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return zero, err
	}
	profile, err := CompileSemanticAnalysisProfileForBackend(backend, operation, source.Width, source.Height, source.Inputs.Alpha != nil, inputBytes, pins, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	task, err := TaskID(workflow, operation, ordinal)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey(operation, ordinal)
	if err != nil {
		return zero, err
	}
	entries := append(append([]Entry{}, prepared.Inputs...), Entry{params, "Params"})
	closure, err := compileCaptureInventory(task, operation, prepared.Params, params, entries)
	if err != nil {
		return zero, err
	}
	closure.Direct = []Ref{params, binding.Source, source.Inputs.Pixels}
	sort.Slice(closure.Direct, func(i, j int) bool {
		return bytes.Compare(closure.Direct[i].ArtifactID[:], closure.Direct[j].ArtifactID[:]) < 0
	})
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenSemanticAnalysisTask{task: task, key: key, operation: operation, profile: string(profile), params: params, closure: closure}, nil
}
func (f FrozenSemanticAnalysisTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid([]byte(f.profile)) || (f.operation != "scene.parse" && f.operation != "depth.predict") {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: f.operation, OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: json.RawMessage(f.profile)}, nil
}
func (f FrozenSemanticAnalysisTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, json.RawMessage(f.profile), f.key, f.closure, built)
}
func PublishSemanticAnalysisTask(ctx context.Context, f FrozenSemanticAnalysisTask, publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, f.task, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return f.BindPublishedTransport(ctx, built)
}
func (f FrozenSemanticAnalysisTask) VerifyInputInventory(ctx context.Context, entries []Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := f.DraftTask(); err != nil {
		return err
	}
	if len(entries) < 3 || len(entries) > 4097 {
		return ErrInvalid
	}
	owned := append([]Entry(nil), entries...)
	seen := map[uuid.UUID]bool{}
	for _, entry := range owned {
		if !validEntry(entry) || seen[entry.Ref.ArtifactID] {
			return ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	actual, err := compileCaptureInventory(f.task, f.operation, f.closure.ParamsJCS, f.params, owned)
	if err != nil {
		return err
	}
	if actual.InputDigest != f.closure.InputDigest {
		return ErrConflict
	}
	return ctx.Err()
}
func (f FrozenSemanticAnalysisTask) ParamsByteCount() (int64, error) {
	if _, err := f.DraftTask(); err != nil {
		return 0, err
	}
	return int64(len(f.closure.ParamsJCS)), nil
}

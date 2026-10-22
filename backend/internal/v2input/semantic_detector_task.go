package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// FrozenSemanticDetectorTask owns the SFD profile and complete input closure.
// Source/model authorization and locked SQL admission are separate requirements.
type FrozenSemanticDetectorTask struct {
	task    uuid.UUID
	key     string
	params  Ref
	profile string
	closure CaptureClosure
}

func CompileSemanticDetectorTask(ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody json.RawMessage, paramsRef Ref, modelLock string, score float64, maxFaces int, sizes []SemanticInputSize, costs SemanticQualifiedCosts, ram, vram, scratch, output int64) (FrozenSemanticDetectorTask, error) {
	return CompileSemanticDetectorTaskForBackend("cuda", ctx, workflow, ordinal, binding, imageBody, paramsRef, modelLock, score, maxFaces, sizes, costs, ram, vram, scratch, output)
}

func CompileSemanticDetectorTaskForBackend(backend string, ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody json.RawMessage, paramsRef Ref, modelLock string, score float64, maxFaces int, sizes []SemanticInputSize, costs SemanticQualifiedCosts, ram, vram, scratch, output int64) (FrozenSemanticDetectorTask, error) {
	var zero FrozenSemanticDetectorTask
	closure, err := CompileSemanticDetectorClosure(ctx, workflow, ordinal, binding, imageBody, paramsRef, modelLock, score, maxFaces)
	if err != nil {
		return zero, err
	}
	prepared, err := PrepareSemanticFaceDetectParams(ctx, binding, imageBody, modelLock, score, maxFaces)
	if err != nil {
		return zero, err
	}
	refs := make([]Ref, len(prepared.Inputs))
	for i, entry := range prepared.Inputs {
		refs[i] = entry.Ref
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return zero, ErrInvalid
	}
	profile, err := CompileSemanticDetectorProfileFromInputsForBackend(backend, int64(image.Canvas.Width), int64(image.Canvas.Height), paramsRef, refs, sizes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	task, err := TaskID(workflow, "face.detect", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey("face.detect", ordinal)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenSemanticDetectorTask{task: task, key: key, params: paramsRef, profile: string(profile), closure: closure}, nil
}

func (f FrozenSemanticDetectorTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid([]byte(f.profile)) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "face.detect", OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: json.RawMessage(f.profile)}, nil
}

func PublishSemanticDetectorTask(ctx context.Context, frozen FrozenSemanticDetectorTask, publisher Publisher) (PreparedSemanticTask, error) {
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

func (f FrozenSemanticDetectorTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, json.RawMessage(f.profile), f.key, f.closure, built)
}

// VerifyInputInventory checks a retained catalog descriptor set against the
// exact frozen task inventory. It grants neither catalog nor SQL authority.
func (f FrozenSemanticDetectorTask) VerifyInputInventory(ctx context.Context, entries []Entry) error {
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
	actual, err := compileCaptureInventory(f.task, "face.detect", f.closure.ParamsJCS, f.params, owned)
	if err != nil {
		return err
	}
	if actual.InputDigest != f.closure.InputDigest {
		return ErrConflict
	}
	return ctx.Err()
}

func (f FrozenSemanticDetectorTask) ParamsByteCount() (int64, error) {
	if _, err := f.DraftTask(); err != nil {
		return 0, err
	}
	return int64(len(f.closure.ParamsJCS)), nil
}

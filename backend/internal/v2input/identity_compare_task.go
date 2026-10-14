package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// FrozenIdentityCompareTask owns a complete immutable transport and bounded
// profile. It is deliberately not an admission authority or enablement flag.
type FrozenIdentityCompareTask struct {
	task    uuid.UUID
	key     string
	params  Ref
	profile json.RawMessage
	closure CaptureClosure
}

func CompileIdentityCompareTask(ctx context.Context, workflow uuid.UUID, ordinal int,
	params IdentityCompareParams, paramsRef Ref, documents IdentityCompareDocuments,
	expectedDatasetSHA string, sizes []SemanticInputSize, qualified SemanticQualifiedCosts,
	ramLimit, vramLimit, scratchLimit, outputLimit int64) (FrozenIdentityCompareTask, error) {
	var zero FrozenIdentityCompareTask
	closure, err := CompileIdentityCompareClosure(ctx, workflow, ordinal, params, paramsRef, documents, expectedDatasetSHA)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(documents.After)
	if err != nil {
		return zero, ErrInvalid
	}
	refs := make([]Ref, 0, len(closure.Direct)-1)
	for _, ref := range closure.Direct {
		if ref != paramsRef {
			refs = append(refs, ref)
		}
	}
	profile, err := CompileIdentityCompareProfileFromInputs(image.Canvas.Width, image.Canvas.Height,
		paramsRef, refs, sizes, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
	if err != nil {
		return zero, err
	}
	task, err := TaskID(workflow, "identity.compare", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey("identity.compare", ordinal)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenIdentityCompareTask{task: task, key: key, params: paramsRef,
		profile: append(json.RawMessage(nil), profile...), closure: closure}, nil
}

func (f FrozenIdentityCompareTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil ||
		f.closure.InputDigest == "" || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "identity.compare", OperationVersion: "1",
		ResourceClass: "gpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256,
		InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}

func PublishIdentityCompareTask(ctx context.Context, frozen FrozenIdentityCompareTask,
	publisher Publisher) (PreparedSemanticTask, error) {
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

func (f FrozenIdentityCompareTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, f.profile, f.key, f.closure, built)
}

func (f FrozenIdentityCompareTask) ParamsByteCount() (int64, error) {
	if _, err := f.DraftTask(); err != nil {
		return 0, err
	}
	return int64(len(f.closure.ParamsJCS)), nil
}

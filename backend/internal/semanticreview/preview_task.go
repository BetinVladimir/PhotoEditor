package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenPreviewTask is a non-dispatchable authored task. SQL publication,
// quota reservation, current authority and graph sealing remain mandatory.
type FrozenPreviewTask struct {
	draft   v2input.DraftTask
	params  v2input.Ref
	closure v2input.CaptureClosure
}

func CompileExposurePreviewTask(run uuid.UUID, p ExposurePreviewParams, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin ExposureQualificationPin) (FrozenPreviewTask, error) {
	empty := FrozenPreviewTask{}
	if !validExposureQualification(pin) || p.ModelLockSHA != pin.ModelLockSHA ||
		p.RuntimeLockSHA != pin.RuntimeLockSHA || p.DeviceProfileSHA != pin.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	_, width, height, err := ResolveExposurePreviewDocuments(p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	cost, err := CompileExposurePreviewResources(width, height, owner, limits)
	if err != nil {
		return empty, err
	}
	task, params, closure, err := CompileExposurePreviewInventory(run, p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(ExposureExecutionProfile{"cpu", "v2.2", cost, pin.ModelLockSHA, pin.RuntimeLockSHA, pin.DeviceProfileSHA, pin.DatasetSHA, pin})
	if err != nil {
		return empty, ErrInvalid
	}
	return FrozenPreviewTask{draft: v2input.DraftTask{ID: task, NodeKey: "semantic.preview_local_exposure:" + p.TargetID.String(),
		Operation: "semantic.preview_local_exposure", OperationVersion: "1", ResourceClass: "cpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}, params: params, closure: closure}, nil
}

// DraftTask returns an owned projection; callers cannot mutate the frozen profile.
func (f FrozenPreviewTask) DraftTask() (v2input.DraftTask, error) {
	if f.draft.ID == uuid.Nil || f.params.ArtifactID == uuid.Nil || f.closure.Open == nil || len(f.draft.ExecutionProfile) == 0 {
		return v2input.DraftTask{}, ErrInvalid
	}
	draft := f.draft
	draft.ExecutionProfile = append(json.RawMessage(nil), f.draft.ExecutionProfile...)
	return draft, nil
}

// PublishExposurePreviewInputs publishes only transport chunks/manifest through
// a scoped durable publisher. Params publication and SQL atomic admission/sealing
// remain separate gates; successful object IO does not make the task runnable.
func PublishExposurePreviewInputs(ctx context.Context, f FrozenPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	empty := PreparedExposureTask{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil {
		return empty, ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return empty, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return empty, err
	}
	built, err := v2input.BuildInputClosure(ctx, draft.ID, cursor, publisher)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreparedExposureTask{Graph: v2input.GraphTask{ID: draft.ID, Built: built, Open: f.closure.Open,
		Direct: append([]v2input.Ref(nil), f.closure.Direct...)}, Draft: draft, Params: f.params,
		ParamsJCS: append(json.RawMessage(nil), f.closure.ParamsJCS...)}, nil
}

// PublishExposurePreviewTask first pins the exact Params document, then its
// input transport. The production publisher must durably authorize each call
// against the saved preview intent. Any failure returns no dispatchable result;
// retries use the same deterministic identities and exact bytes.
func PublishExposurePreviewTask(ctx context.Context, f FrozenPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	return publishFrozenDocumentTask(ctx, f, publisher)
}

// publishFrozenDocumentTask handles owned transport only; operation-specific SQL
// authority is required from its scoped publisher and later atomic admission.
func publishFrozenDocumentTask(ctx context.Context, f FrozenPreviewTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	empty := PreparedExposureTask{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil {
		return empty, ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return empty, err
	}
	body := append([]byte(nil), f.closure.ParamsJCS...)
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != f.params.SHA256 {
		return empty, ErrInvalid
	}
	ref, err := publisher.Publish(ctx, draft.ID, 0, "Params", body)
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err != nil {
		return empty, err
	}
	if ref != f.params {
		return empty, ErrInvalid
	}
	return PublishExposurePreviewInputs(ctx, f, publisher)
}

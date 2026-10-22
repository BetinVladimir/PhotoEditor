package v2input

import (
	"context"
	"encoding/json"
)

// FrozenAuthoredNativeReplayTask owns a resolved, complete inventory. This pure
// compiler grants no artifact authority: the source-bound resolver and SQL stage
// must both recheck the immutable consumer before admission.
type FrozenAuthoredNativeReplayTask struct {
	request AuthoredNativeReplayRequest
	closure CaptureClosure
	profile json.RawMessage
}

func CompileAuthoredNativeReplayTask(requestRaw, nativeParams []byte, resolved []Entry, contractsSHA string) (FrozenAuthoredNativeReplayTask, error) {
	var zero FrozenAuthoredNativeReplayTask
	request, err := DecodeAuthoredNativeReplayRequest(requestRaw)
	if err != nil {
		return zero, err
	}
	_, _, task, requestRef, err := request.IDs()
	if err != nil {
		return zero, err
	}
	if len(resolved) < 1 || len(resolved) > MaxEntries {
		return zero, ErrInvalid
	}
	// Do not sort or retain caller-owned backing arrays.
	entries := append([]Entry(nil), resolved...)
	required := []Entry{{requestRef, "AuthoredNativeReplayRequest"}, {request.Original.Task.Params, "Params"},
		{request.Consumer.Params, "Params"}, {request.Consumer.Source.Source, "WorkingImage"},
		{request.GeometryParams, "Params"}, {request.NativeSource.Source, "WorkingImage"},
		{request.Original.Root, "WorkingImage"}, {request.Original.Report, "UpscaleReport"}}
	if request.PhaseReceipt != nil {
		required = append(required, Entry{*request.PhaseReceipt, "AuthoredCanonicalPhaseReceipt"})
	}
	present := make(map[Entry]bool, len(entries))
	for _, entry := range entries {
		present[entry] = true
	}
	for _, entry := range required {
		if !present[entry] {
			return zero, ErrInvalid
		}
	}
	// The actual native Params remain byte-identical, including model and source.
	closure, err := compileCaptureInventory(task, "image.upscale", json.RawMessage(append([]byte(nil), nativeParams...)), request.Original.Task.Params, entries)
	if err != nil {
		return zero, err
	}
	profile, err := AuthoredNativeReplayTransportProfile(request.Original.Task.ProfileJCS, len(closure.Direct), contractsSHA)
	if err != nil {
		return zero, err
	}
	return FrozenAuthoredNativeReplayTask{request: request, closure: closure, profile: json.RawMessage(profile)}, nil
}

func (f FrozenAuthoredNativeReplayTask) DraftTask() (DraftTask, error) {
	_, _, task, _, err := f.request.IDs()
	if err != nil || f.closure.Open == nil || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: task, NodeKey: "image.upscale:0", Operation: "image.upscale", OperationVersion: "1", ResourceClass: "gpu",
		ParamsArtifactID: f.request.Original.Task.Params.ArtifactID, ParamsSHA: f.request.Original.Task.Params.SHA256,
		InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}
func (f FrozenAuthoredNativeReplayTask) PublishTransport(ctx context.Context, publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	draft, err := f.DraftTask()
	if err != nil || publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, draft.ID, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, draft.ID, f.request.Original.Task.Params, f.profile, draft.NodeKey, f.closure, built)
}

// InventoryCount is the complete frozen unique entry count, including request.
func (f FrozenAuthoredNativeReplayTask) InventoryCount() int { return len(f.closure.Direct) }

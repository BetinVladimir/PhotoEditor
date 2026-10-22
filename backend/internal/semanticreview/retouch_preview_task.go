package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type RetouchExecutionProfile struct {
	Backend          string                   `json:"backend"`
	RendererVersion  string                   `json:"renderer_version"`
	ResourceEstimate ExposureResourceEstimate `json:"resource_estimate"`
	ModelLockID      string                   `json:"model_lock_id"`
	ModelLockSHA     string                   `json:"model_lock_sha"`
	RuntimeLockSHA   string                   `json:"runtime_lock_sha"`
	DeviceProfileSHA string                   `json:"device_profile_sha"`
	DatasetSHA       string                   `json:"dataset_sha"`
	Qualification    RetouchQualificationPin  `json:"qualification"`
	RetouchContext   v2input.Ref              `json:"retouch_context"`
}

func CompileRetouchPreviewTask(run uuid.UUID, p RetouchPreviewParams, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin RetouchQualificationPin, contextRef v2input.Ref, contextBody []byte) (FrozenRetouchPreviewTask, error) {
	empty := FrozenRetouchPreviewTask{}
	if !validRetouchQualification(pin) || p.ModelLockSHA != pin.ModelLockSHA ||
		p.RuntimeLockSHA != pin.RuntimeLockSHA || p.DeviceProfileSHA != pin.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	_, width, height, err := ResolveRetouchPreviewDocuments(p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	cost, err := CompileRetouchPreviewResources(width, height, owner, limits)
	if err != nil {
		return empty, err
	}
	task, params, closure, err := CompileRetouchPreviewInventory(run, p, candidate, intent, docs, contextRef, contextBody)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(RetouchExecutionProfile{RetouchContext: contextRef, Backend: pin.Backend, RendererVersion: "v2.2", ResourceEstimate: cost, ModelLockID: pin.ModelLockSHA, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, Qualification: pin})
	if err != nil {
		return empty, ErrInvalid
	}
	return FrozenRetouchPreviewTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{ID: task, NodeKey: "semantic.preview_retouch_region:" + p.TargetID.String(),
		Operation: "semantic.preview_retouch_region", OperationVersion: "1", ResourceClass: "cpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}, params: params, closure: closure}}, nil
}

// FrozenRetouchPreviewTask cannot be confused with an exposure-only task.
type FrozenRetouchPreviewTask struct{ frozen FrozenPreviewTask }

func (f FrozenRetouchPreviewTask) DraftTask() (v2input.DraftTask, error) {
	draft, err := f.frozen.DraftTask()
	if err != nil || draft.Operation != "semantic.preview_retouch_region" || draft.ResourceClass != "cpu" {
		return v2input.DraftTask{}, ErrInvalid
	}
	return draft, nil
}

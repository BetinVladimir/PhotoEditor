package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RemovalQualificationPin is structure only; SQL and exact reports grant authority.
type RemovalQualificationPin v2input.SemanticQualificationPin

type RemovalExecutionProfile struct {
	Backend          string                   `json:"backend"`
	RendererVersion  string                   `json:"renderer_version"`
	ResourceEstimate ExposureResourceEstimate `json:"resource_estimate"`
	ModelLockID      string                   `json:"model_lock_id"`
	ModelLockSHA     string                   `json:"model_lock_sha"`
	RuntimeLockSHA   string                   `json:"runtime_lock_sha"`
	DeviceProfileSHA string                   `json:"device_profile_sha"`
	DatasetSHA       string                   `json:"dataset_sha"`
	Qualification    RemovalQualificationPin  `json:"qualification"`
}

func validRemovalQualification(p RemovalQualificationPin) bool {
	if p.Backend != "cpu" && p.Backend != "cuda" && p.Backend != "mps" {
		return false
	}
	common := ExposureQualificationPin(p)
	common.Backend = "cpu"
	return validExposureQualification(common)
}

func CompileRemovalPreviewTask(run uuid.UUID, p RemovalPreviewParams, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin RemovalQualificationPin, measuredDeviceBytes int64) (FrozenRemovalPreviewTask, error) {
	empty := FrozenRemovalPreviewTask{}
	if !validRemovalQualification(pin) || p.ModelLockSHA != pin.ModelLockSHA ||
		p.RuntimeLockSHA != pin.RuntimeLockSHA || p.DeviceProfileSHA != pin.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	_, width, height, err := ResolveRemovalPreviewDocuments(p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	cost, err := CompileRemovalPreviewResources(width, height, owner, limits, pin.Backend, measuredDeviceBytes)
	if err != nil {
		return empty, err
	}
	task, params, closure, err := CompileRemovalPreviewInventory(run, p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(RemovalExecutionProfile{Backend: pin.Backend, RendererVersion: "v2.2", ResourceEstimate: cost, ModelLockID: pin.ModelLockSHA, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, Qualification: pin})
	if err != nil {
		return empty, ErrInvalid
	}
	return FrozenRemovalPreviewTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{ID: task, NodeKey: "semantic.preview_remove_object:" + p.TargetID.String(),
		Operation: "semantic.preview_remove_object", OperationVersion: "1", ResourceClass: "gpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}, params: params, closure: closure}}, nil
}

// FrozenRemovalPreviewTask cannot be confused with an exposure-only task.
type FrozenRemovalPreviewTask struct{ frozen FrozenPreviewTask }

func (f FrozenRemovalPreviewTask) DraftTask() (v2input.DraftTask, error) {
	draft, err := f.frozen.DraftTask()
	if err != nil || draft.Operation != "semantic.preview_remove_object" || draft.ResourceClass != "gpu" {
		return v2input.DraftTask{}, ErrInvalid
	}
	return draft, nil
}

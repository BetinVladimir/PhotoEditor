package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BackgroundCleanupQualificationPin is structure only; SQL and exact reports grant authority.
type BackgroundCleanupQualificationPin v2input.SemanticQualificationPin

type BackgroundCleanupExecutionProfile struct {
	Backend          string                            `json:"backend"`
	RendererVersion  string                            `json:"renderer_version"`
	ResourceEstimate ExposureResourceEstimate          `json:"resource_estimate"`
	ModelLockID      string                            `json:"model_lock_id"`
	ModelLockSHA     string                            `json:"model_lock_sha"`
	RuntimeLockSHA   string                            `json:"runtime_lock_sha"`
	DeviceProfileSHA string                            `json:"device_profile_sha"`
	DatasetSHA       string                            `json:"dataset_sha"`
	Qualification    BackgroundCleanupQualificationPin `json:"qualification"`
}

func validBackgroundCleanupQualification(p BackgroundCleanupQualificationPin) bool {
	if p.Backend != "cpu" && p.Backend != "cuda" && p.Backend != "mps" {
		return false
	}
	common := ExposureQualificationPin(p)
	common.Backend = "cpu"
	return validExposureQualification(common)
}

func CompileBackgroundCleanupPreviewTask(run uuid.UUID, p BackgroundCleanupPreviewParams, candidate, intent []byte,
	docs ExposureSourceDocuments, owner ExposureOwnerCosts, limits ExposureResourceEstimate,
	pin BackgroundCleanupQualificationPin, measuredDeviceBytes int64) (FrozenBackgroundCleanupPreviewTask, error) {
	empty := FrozenBackgroundCleanupPreviewTask{}
	if !validBackgroundCleanupQualification(pin) || p.ModelLockSHA != pin.ModelLockSHA ||
		p.RuntimeLockSHA != pin.RuntimeLockSHA || p.DeviceProfileSHA != pin.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	_, width, height, err := ResolveBackgroundCleanupPreviewDocuments(p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	cost, err := CompileBackgroundCleanupPreviewResources(width, height, owner, limits, pin.Backend, measuredDeviceBytes)
	if err != nil {
		return empty, err
	}
	task, params, closure, err := CompileBackgroundCleanupPreviewInventory(run, p, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(BackgroundCleanupExecutionProfile{Backend: pin.Backend, RendererVersion: "v2.2", ResourceEstimate: cost, ModelLockID: pin.ModelLockSHA, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, Qualification: pin})
	if err != nil {
		return empty, ErrInvalid
	}
	return FrozenBackgroundCleanupPreviewTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{ID: task, NodeKey: "semantic.preview_background_cleanup:" + p.TargetID.String(),
		Operation: "semantic.preview_background_cleanup", OperationVersion: "1", ResourceClass: "gpu",
		ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}, params: params, closure: closure}}, nil
}

// FrozenBackgroundCleanupPreviewTask cannot be confused with an exposure-only task.
type FrozenBackgroundCleanupPreviewTask struct{ frozen FrozenPreviewTask }

func (f FrozenBackgroundCleanupPreviewTask) DraftTask() (v2input.DraftTask, error) {
	draft, err := f.frozen.DraftTask()
	if err != nil || draft.Operation != "semantic.preview_background_cleanup" || draft.ResourceClass != "gpu" {
		return v2input.DraftTask{}, ErrInvalid
	}
	return draft, nil
}

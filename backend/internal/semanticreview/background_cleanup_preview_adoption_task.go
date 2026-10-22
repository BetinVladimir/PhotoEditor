package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenShownBackgroundCleanupTask is a copy operation with owned profile and closure.
// Publication does not commit a decision, grant a lease or activate the child.
type FrozenShownBackgroundCleanupTask struct{ frozen FrozenPreviewTask }

func CompileShownBackgroundCleanupTask(compiled CompiledShownBackgroundCleanupAdoption, approved ApprovedTaskDraft,
	physical PhysicalBackgroundCleanupPreview, candidate, intent []byte, docs ExposureSourceDocuments,
	owner ExposureOwnerCosts, limits ExposureResourceEstimate, pin ExposureQualificationPin) (FrozenShownBackgroundCleanupTask, error) {
	empty := FrozenShownBackgroundCleanupTask{}
	p := compiled.params
	if !validExposureQualification(pin) || pin.ModelLockSHA != p.ModelLockSHA || pin.RuntimeLockSHA != p.RuntimeLockSHA || pin.DeviceProfileSHA != p.DeviceProfileSHA {
		return empty, ErrInvalid
	}
	closure, err := CompileShownBackgroundCleanupAdoptionInventory(compiled, approved, physical, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	_, width, height, err := ResolveBackgroundCleanupPreviewDocuments(physical.linkage.Presentation.Params, candidate, intent, docs)
	if err != nil {
		return empty, err
	}
	costs, err := CompileShownBackgroundCleanupAdoptionResources(width, height, owner, limits)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(ExposureExecutionProfile{"cpu", "v2.2", costs, pin.ModelLockSHA, pin.RuntimeLockSHA, pin.DeviceProfileSHA, pin.DatasetSHA, pin})
	if err != nil {
		return empty, ErrInvalid
	}
	draft := v2input.DraftTask{ID: compiled.TaskID, NodeKey: "semantic.adopt_shown_background_cleanup:" + p.TargetID.String(),
		Operation: "semantic.adopt_shown_background_cleanup", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: compiled.ParamsRef.ArtifactID,
		ParamsSHA: compiled.ParamsRef.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}
	return FrozenShownBackgroundCleanupTask{frozen: FrozenPreviewTask{draft: draft, params: compiled.ParamsRef, closure: closure}}, nil
}

func (f FrozenShownBackgroundCleanupTask) DraftTask() (v2input.DraftTask, error) {
	draft, err := f.frozen.DraftTask()
	if err != nil || draft.Operation != "semantic.adopt_shown_background_cleanup" || draft.ID == uuid.Nil {
		return v2input.DraftTask{}, ErrInvalid
	}
	draft.ExecutionProfile = append(json.RawMessage(nil), draft.ExecutionProfile...)
	return draft, nil
}

// PublishShownBackgroundCleanupTask uses the same deterministic Params/chunk/manifest
// transport as previews. The publisher must authorize the Apply publication
// scope; no preview-run authority may substitute for child admission.
func PublishShownBackgroundCleanupTask(ctx context.Context, f FrozenShownBackgroundCleanupTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedExposureTask{}, err
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedExposureTask{}, err
	}
	return publishFrozenDocumentTask(ctx, f.frozen, publisher)
}

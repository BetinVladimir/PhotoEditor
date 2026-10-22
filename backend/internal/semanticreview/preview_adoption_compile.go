package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// CompiledShownExposureAdoption owns publication bytes, not an admitted task.
// It deliberately cannot be passed to the semantic.local_exposure compiler.
type CompiledShownExposureAdoption struct {
	TaskID    uuid.UUID
	ParamsRef v2input.Ref
	params    ShownExposureAdoptionParams
	body      []byte
}

func (c CompiledShownExposureAdoption) Body() []byte                        { return append([]byte(nil), c.body...) }
func (c CompiledShownExposureAdoption) Params() ShownExposureAdoptionParams { return c.params }

// CompileShownExposureAdoption joins a transaction-locked selection with a
// physically verified preview and exact approved-edit bytes. Caller must still
// publish the full closure and atomically install the copy task under the lock.
func CompileShownExposureAdoption(child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedPreviewSelection, physical PhysicalExposurePreview) (CompiledShownExposureAdoption, error) {
	empty := CompiledShownExposureAdoption{}
	if !physical.verified || child == uuid.Nil || selected.PreviewRunID == uuid.Nil || selected.TargetID == uuid.Nil ||
		approved.TargetID != selected.TargetID || approved.Kind != "local_exposure" || len(approved.Body) < 2 || len(approved.Body) > 1048576 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(approved.Body)
	if hex.EncodeToString(sum[:]) != approved.Params.SHA256 {
		return empty, ErrInvalid
	}
	var edit ApprovedEdit
	if decodeStrict(approved.Body, &edit) != nil || edit.SchemaVersion != 1 || edit.Kind != "local_exposure" ||
		edit.TargetID != selected.TargetID || !edit.PreservePersons {
		return empty, ErrInvalid
	}
	link := physical.linkage
	preview := link.Presentation.Params
	_, digest, err := FreezeExposurePreviewParams(preview)
	if err != nil {
		return empty, err
	}
	expected := ExposurePreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: link.Document.TaskID,
		ResultID: link.Document.ResultID, Attempt: link.Document.Attempt, Fence: link.Document.Fence,
		Presentation: link.Document.Source.Ref, WorkingImage: link.Presentation.WorkingImage,
		Pixels: link.Presentation.Pixels, NumericalEvidence: link.Presentation.NumericalEvidence}
	if selected.Receipt != expected || preview.TargetID != selected.TargetID || child == preview.WorkflowID || child == selected.PreviewRunID ||
		edit.Source != preview.Source || edit.Intent != preview.Intent || edit.Candidate != preview.Candidate ||
		link.Document.TaskID != uuid.NewSHA1(selected.PreviewRunID, []byte("semantic.preview_local_exposure:"+selected.TargetID.String())) {
		return empty, ErrInvalid
	}
	for _, root := range []v2input.Ref{preview.Plan, preview.Intent, preview.Candidate, preview.Source.Source, preview.Protection} {
		if approved.Params.ArtifactID == root.ArtifactID {
			return empty, ErrInvalid
		}
	}
	p := ShownExposureAdoptionParams{SchemaVersion: 1, TenantID: preview.TenantID, ChildRunID: child, TargetID: selected.TargetID,
		ApprovedEdit: approved.Params, PreviewRunID: selected.PreviewRunID, PreviewTaskID: expected.TaskID, PreviewResultID: expected.ResultID,
		PreviewAttempt: expected.Attempt, PreviewFence: expected.Fence, Presentation: expected.Presentation, NumericalEvidence: expected.NumericalEvidence,
		WorkingImage: expected.WorkingImage, Pixels: expected.Pixels, ModelLockSHA: preview.ModelLockSHA, RuntimeLockSHA: preview.RuntimeLockSHA, DeviceProfileSHA: preview.DeviceProfileSHA}
	body, sha, err := FreezeShownExposureAdoptionParams(p)
	if err != nil {
		return empty, err
	}
	task := uuid.NewSHA1(child, []byte("semantic.adopt_shown_exposure:"+selected.TargetID.String()))
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: sha}
	// The approved edit and copy Params must remain separate immutable documents.
	for _, r := range []v2input.Ref{p.ApprovedEdit, p.Presentation, p.NumericalEvidence, p.WorkingImage, p.Pixels} {
		if r.ArtifactID == ref.ArtifactID || r.ArtifactID == task {
			return empty, ErrInvalid
		}
	}
	return CompiledShownExposureAdoption{TaskID: task, ParamsRef: ref, params: p, body: body}, nil
}

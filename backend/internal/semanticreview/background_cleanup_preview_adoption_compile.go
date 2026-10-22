package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// CompiledShownBackgroundCleanupAdoption owns publication bytes, not an admitted task.
// Its distinct type cannot be passed to the inference task compiler.
type CompiledShownBackgroundCleanupAdoption struct {
	TaskID    uuid.UUID
	ParamsRef v2input.Ref
	params    ShownBackgroundCleanupAdoptionParams
	body      []byte
}

func (c CompiledShownBackgroundCleanupAdoption) Body() []byte { return append([]byte(nil), c.body...) }
func (c CompiledShownBackgroundCleanupAdoption) Params() ShownBackgroundCleanupAdoptionParams {
	return c.params
}

// CompileShownBackgroundCleanupAdoption joins a transaction-locked selection with a
// physically verified preview and exact approved-edit bytes. Caller must still
// publish the full closure and atomically install the copy task under the lock.
func CompileShownBackgroundCleanupAdoption(child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedBackgroundCleanupPreviewSelection, physical PhysicalBackgroundCleanupPreview) (CompiledShownBackgroundCleanupAdoption, error) {
	empty := CompiledShownBackgroundCleanupAdoption{}
	if !physical.verified || child == uuid.Nil || selected.PreviewRunID == uuid.Nil || selected.TargetID == uuid.Nil ||
		approved.TargetID != selected.TargetID || approved.Kind != "background_cleanup" || len(approved.Body) < 2 || len(approved.Body) > 1048576 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(approved.Body)
	if hex.EncodeToString(sum[:]) != approved.Params.SHA256 {
		return empty, ErrInvalid
	}
	var edit ApprovedEdit
	if decodeStrict(approved.Body, &edit) != nil || edit.SchemaVersion != 1 || edit.Kind != "background_cleanup" ||
		edit.TargetID != selected.TargetID || !edit.PreservePersons || edit.EV != nil {
		return empty, ErrInvalid
	}
	link := physical.linkage
	preview := link.Presentation.Params
	_, digest, err := FreezeBackgroundCleanupPreviewParams(preview)
	if err != nil {
		return empty, err
	}
	expected := BackgroundCleanupPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: link.Document.TaskID,
		ResultID: link.Document.ResultID, Attempt: link.Document.Attempt, Fence: link.Document.Fence,
		Presentation: link.Document.Source.Ref, WorkingImage: link.Presentation.WorkingImage,
		Pixels: link.Presentation.Pixels, NumericalEvidence: link.Presentation.NumericalEvidence}
	if selected.Receipt != expected || preview.TargetID != selected.TargetID || child == preview.WorkflowID || child == selected.PreviewRunID ||
		edit.Source != preview.Source || edit.Intent != preview.Intent || edit.Candidate != preview.Candidate ||
		link.Document.TaskID != uuid.NewSHA1(selected.PreviewRunID, []byte("semantic.preview_background_cleanup:"+selected.TargetID.String())) {
		return empty, ErrInvalid
	}
	for _, root := range []v2input.Ref{preview.Plan, preview.Intent, preview.Candidate, preview.Source.Source, preview.Protection} {
		if approved.Params.ArtifactID == root.ArtifactID {
			return empty, ErrInvalid
		}
	}
	p := ShownBackgroundCleanupAdoptionParams{SchemaVersion: 1, TenantID: preview.TenantID, ChildRunID: child, TargetID: selected.TargetID,
		ApprovedEdit: approved.Params, PreviewRunID: selected.PreviewRunID, PreviewTaskID: expected.TaskID, PreviewResultID: expected.ResultID,
		PreviewAttempt: expected.Attempt, PreviewFence: expected.Fence, Presentation: expected.Presentation, NumericalEvidence: expected.NumericalEvidence,
		WorkingImage: expected.WorkingImage, Pixels: expected.Pixels, ModelLockSHA: preview.ModelLockSHA, RuntimeLockSHA: preview.RuntimeLockSHA, DeviceProfileSHA: preview.DeviceProfileSHA}
	body, sha, err := FreezeShownBackgroundCleanupAdoptionParams(p)
	if err != nil {
		return empty, err
	}
	task := uuid.NewSHA1(child, []byte("semantic.adopt_shown_background_cleanup:"+selected.TargetID.String()))
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: sha}
	// The approved edit and copy Params must remain separate immutable documents.
	for _, r := range []v2input.Ref{p.ApprovedEdit, p.Presentation, p.NumericalEvidence, p.WorkingImage, p.Pixels} {
		if r.ArtifactID == ref.ArtifactID || r.ArtifactID == task {
			return empty, ErrInvalid
		}
	}
	return CompiledShownBackgroundCleanupAdoption{TaskID: task, ParamsRef: ref, params: p, body: body}, nil
}

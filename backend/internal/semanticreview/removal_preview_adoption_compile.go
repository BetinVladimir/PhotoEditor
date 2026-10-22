package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// CompiledShownRemovalAdoption owns publication bytes, not an admitted task.
// It deliberately cannot be passed to the semantic.remove_object compiler.
type CompiledShownRemovalAdoption struct {
	TaskID    uuid.UUID
	ParamsRef v2input.Ref
	params    ShownRemovalAdoptionParams
	body      []byte
}

func (c CompiledShownRemovalAdoption) Body() []byte                       { return append([]byte(nil), c.body...) }
func (c CompiledShownRemovalAdoption) Params() ShownRemovalAdoptionParams { return c.params }

// CompileShownRemovalAdoption joins a transaction-locked selection with a
// physically verified preview and exact approved-edit bytes. Caller must still
// publish the full closure and atomically install the copy task under the lock.
func CompileShownRemovalAdoption(child uuid.UUID, approved ApprovedTaskDraft,
	selected LockedRemovalPreviewSelection, physical PhysicalRemovalPreview) (CompiledShownRemovalAdoption, error) {
	empty := CompiledShownRemovalAdoption{}
	if !physical.verified || child == uuid.Nil || selected.PreviewRunID == uuid.Nil || selected.TargetID == uuid.Nil ||
		approved.TargetID != selected.TargetID || approved.Kind != "remove_object" || len(approved.Body) < 2 || len(approved.Body) > 1048576 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(approved.Body)
	if hex.EncodeToString(sum[:]) != approved.Params.SHA256 {
		return empty, ErrInvalid
	}
	var edit ApprovedEdit
	if decodeStrict(approved.Body, &edit) != nil || edit.SchemaVersion != 1 || edit.Kind != "remove_object" ||
		edit.TargetID != selected.TargetID || !edit.PreservePersons || edit.EV != nil {
		return empty, ErrInvalid
	}
	link := physical.linkage
	preview := link.Presentation.Params
	_, digest, err := FreezeRemovalPreviewParams(preview)
	if err != nil {
		return empty, err
	}
	expected := RemovalPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: link.Document.TaskID,
		ResultID: link.Document.ResultID, Attempt: link.Document.Attempt, Fence: link.Document.Fence,
		Presentation: link.Document.Source.Ref, WorkingImage: link.Presentation.WorkingImage,
		Pixels: link.Presentation.Pixels, NumericalEvidence: link.Presentation.NumericalEvidence}
	if selected.Receipt != expected || preview.TargetID != selected.TargetID || child == preview.WorkflowID || child == selected.PreviewRunID ||
		edit.Source != preview.Source || edit.Intent != preview.Intent || edit.Candidate != preview.Candidate ||
		link.Document.TaskID != uuid.NewSHA1(selected.PreviewRunID, []byte("semantic.preview_remove_object:"+selected.TargetID.String())) {
		return empty, ErrInvalid
	}
	for _, root := range []v2input.Ref{preview.Plan, preview.Intent, preview.Candidate, preview.Source.Source, preview.Protection} {
		if approved.Params.ArtifactID == root.ArtifactID {
			return empty, ErrInvalid
		}
	}
	p := ShownRemovalAdoptionParams{SchemaVersion: 1, TenantID: preview.TenantID, ChildRunID: child, TargetID: selected.TargetID,
		ApprovedEdit: approved.Params, PreviewRunID: selected.PreviewRunID, PreviewTaskID: expected.TaskID, PreviewResultID: expected.ResultID,
		PreviewAttempt: expected.Attempt, PreviewFence: expected.Fence, Presentation: expected.Presentation, NumericalEvidence: expected.NumericalEvidence,
		WorkingImage: expected.WorkingImage, Pixels: expected.Pixels, ModelLockSHA: preview.ModelLockSHA, RuntimeLockSHA: preview.RuntimeLockSHA, DeviceProfileSHA: preview.DeviceProfileSHA}
	body, sha, err := FreezeShownRemovalAdoptionParams(p)
	if err != nil {
		return empty, err
	}
	task := uuid.NewSHA1(child, []byte("semantic.adopt_shown_removal:"+selected.TargetID.String()))
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: sha}
	// The approved edit and copy Params must remain separate immutable documents.
	for _, r := range []v2input.Ref{p.ApprovedEdit, p.Presentation, p.NumericalEvidence, p.WorkingImage, p.Pixels} {
		if r.ArtifactID == ref.ArtifactID || r.ArtifactID == task {
			return empty, ErrInvalid
		}
	}
	return CompiledShownRemovalAdoption{TaskID: task, ParamsRef: ref, params: p, body: body}, nil
}

package semanticreview

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// CompileShownExposureAdoptionInventory derives every readable dependency from
// verified proposal documents and the selected presentation. It grants no SQL
// authority and must be sealed with the receipt in the Apply transaction.
func CompileShownExposureAdoptionInventory(compiled CompiledShownExposureAdoption,
	approved ApprovedTaskDraft, physical PhysicalExposurePreview, candidateRaw, intentRaw []byte,
	docs ExposureSourceDocuments) (v2input.CaptureClosure, error) {
	invalid := func() (v2input.CaptureClosure, error) { return v2input.CaptureClosure{}, ErrInvalid }
	p := compiled.params
	selected := LockedPreviewSelection{TargetID: p.TargetID, PreviewRunID: p.PreviewRunID,
		Receipt: ExposurePreviewReceipt{SchemaVersion: 1, TaskID: p.PreviewTaskID, ResultID: p.PreviewResultID,
			Attempt: p.PreviewAttempt, Fence: p.PreviewFence, Presentation: p.Presentation, WorkingImage: p.WorkingImage,
			Pixels: p.Pixels, NumericalEvidence: p.NumericalEvidence}}
	_, previewSHA, err := FreezeExposurePreviewParams(physical.linkage.Presentation.Params)
	if err != nil {
		return invalid()
	}
	selected.Receipt.ParamsSHA = previewSHA
	rebuilt, err := CompileShownExposureAdoption(p.ChildRunID, approved, selected, physical)
	if err != nil || rebuilt.TaskID != compiled.TaskID || rebuilt.ParamsRef != compiled.ParamsRef || !bytes.Equal(rebuilt.body, compiled.body) {
		return invalid()
	}
	preview := physical.linkage.Presentation.Params
	deps, _, _, err := ResolveExposurePreviewDocuments(preview, candidateRaw, intentRaw, docs)
	if err != nil {
		return invalid()
	}
	candidate, err := v2input.DecodeSemanticCandidate(candidateRaw)
	if err != nil {
		return invalid()
	}
	intent, err := v2input.DecodeSemanticEditIntent(intentRaw)
	if err != nil {
		return invalid()
	}
	var edit ApprovedEdit
	if decodeStrict(approved.Body, &edit) != nil || edit.Mask != *candidate.Mask || edit.Transform != *candidate.Transform ||
		edit.Strength != intent.Strength || edit.EV == nil || intent.EV == nil || *edit.EV != *intent.EV ||
		(edit.Prompt == nil) != (intent.Prompt == nil) {
		return invalid()
	}
	if edit.Prompt != nil && *edit.Prompt != *intent.Prompt {
		return invalid()
	}
	entries := []v2input.Entry{{Ref: compiled.ParamsRef, Contract: "Params"}, {Ref: p.ApprovedEdit, Contract: "ApprovedSemanticEdit"},
		{Ref: p.Presentation, Contract: "ExposurePreviewPresentation"}, {Ref: p.NumericalEvidence, Contract: "ExposurePreviewNumericalEvidence"},
		{Ref: p.WorkingImage, Contract: "WorkingImage"}, {Ref: p.Pixels, Contract: "NpyArray"},
		{Ref: preview.Plan, Contract: "SemanticPlan"}, {Ref: preview.Intent, Contract: "EditIntent"}, {Ref: preview.Candidate, Contract: "TransferCandidate"},
		{Ref: preview.Source.Source, Contract: "WorkingImage"}, {Ref: preview.Protection, Contract: "MaskRef"},
		{Ref: *candidate.Mask, Contract: "MaskRef"}, {Ref: *candidate.Transform, Contract: "ResolvedTransform"},
		{Ref: deps.Reference, Contract: "WorkingImage"}, {Ref: deps.Source.Pixels, Contract: "NpyArray"},
		{Ref: deps.ReferenceImage.Pixels, Contract: "NpyArray"}, {Ref: deps.MaskPixels, Contract: "NpyArray"}, {Ref: deps.ProtectionPixels, Contract: "NpyArray"}}
	for _, ref := range []v2input.Ref{physical.linkage.Presentation.Before, physical.linkage.Presentation.After,
		physical.linkage.Presentation.Affected, physical.linkage.Presentation.Protected} {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "EncodedImage"})
	}
	for _, alpha := range []*v2input.Ref{deps.Source.Alpha, deps.ReferenceImage.Alpha} {
		if alpha != nil {
			entries = append(entries, v2input.Entry{Ref: *alpha, Contract: "NpyArray"})
		}
	}
	for _, ref := range deps.Geometry {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "NpyArray"})
	}
	if compiled.TaskID == uuid.Nil {
		return invalid()
	}
	return v2input.FreezeInputInventory(compiled.TaskID, "semantic.adopt_shown_exposure", compiled.body, compiled.ParamsRef, entries)
}

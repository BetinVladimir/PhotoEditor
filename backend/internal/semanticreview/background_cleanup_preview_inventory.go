package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// CompileBackgroundCleanupPreviewInventory freezes every readable proposal dependency.
// Its deterministic namespace belongs to the preview run, never an Apply child.
// Neither a closure nor its digest authorizes publication or dispatch.
func CompileBackgroundCleanupPreviewInventory(run uuid.UUID, p BackgroundCleanupPreviewParams,
	candidateBody, intentBody []byte, docs ExposureSourceDocuments) (uuid.UUID, v2input.Ref, v2input.CaptureClosure, error) {
	invalid := func() (uuid.UUID, v2input.Ref, v2input.CaptureClosure, error) {
		return uuid.Nil, v2input.Ref{}, v2input.CaptureClosure{}, ErrInvalid
	}
	if run == uuid.Nil || run == p.WorkflowID {
		return invalid()
	}
	body, digest, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil {
		return invalid()
	}
	deps, _, _, err := ResolveBackgroundCleanupPreviewDocuments(p, candidateBody, intentBody, docs)
	if err != nil {
		return invalid()
	}
	candidate, err := v2input.DecodeSemanticCandidate(candidateBody)
	if err != nil {
		return invalid()
	}
	task := uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+p.TargetID.String()))
	params := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: digest}
	entries := []v2input.Entry{{Ref: params, Contract: "Params"}, {Ref: p.Plan, Contract: "SemanticPlan"},
		{Ref: p.Intent, Contract: "EditIntent"}, {Ref: p.Candidate, Contract: "TransferCandidate"},
		{Ref: p.Source.Source, Contract: "WorkingImage"}, {Ref: p.Protection, Contract: "MaskRef"},
		{Ref: *candidate.Mask, Contract: "MaskRef"}, {Ref: *candidate.Transform, Contract: "ResolvedTransform"},
		{Ref: deps.Reference, Contract: "WorkingImage"}, {Ref: deps.Source.Pixels, Contract: "NpyArray"},
		{Ref: deps.ReferenceImage.Pixels, Contract: "NpyArray"}, {Ref: deps.MaskPixels, Contract: "NpyArray"},
		{Ref: deps.ProtectionPixels, Contract: "NpyArray"}}
	for _, alpha := range []*v2input.Ref{deps.Source.Alpha, deps.ReferenceImage.Alpha} {
		if alpha != nil {
			entries = append(entries, v2input.Entry{Ref: *alpha, Contract: "NpyArray"})
		}
	}
	for _, ref := range deps.Geometry {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "NpyArray"})
	}
	closure, err := v2input.FreezeInputInventory(task, "semantic.preview_background_cleanup", body, params, entries)
	if err != nil {
		return invalid()
	}
	return task, params, closure, nil
}

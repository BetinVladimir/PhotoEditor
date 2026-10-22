package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// nativeMultiGeometry uses the real operation-specific report, registration,
// matrix/dense geometry and source occurrence predicates. It proves JSON
// geometry only; native array replay/qualification remains an independent gate.
func (s SQLAuthoredGeometryInputCatalog) nativeMultiGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, phase AuthoredPhaseReceipt) (v2draft.DocumentGeometry, error) {
	empty := v2draft.DocumentGeometry{}
	if phase.Geometry == nil {
		return empty, v2input.ErrInvalid
	}
	native := phase.Geometry
	read := func(ref v2input.Ref) ([]byte, error) {
		return s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, ref)
	}
	params, err := read(native.Task.Params)
	if err != nil {
		return empty, err
	}
	binding := AdoptedMultiBinding{ResultID: native.Task.ResultID, Result: native.Root, Run: native.Task.RunID, Operation: native.Task.Operation, ParamsJCS: string(params)}
	frames, reference, _, err := binding.Inputs()
	if err != nil || reference < 0 || reference >= len(frames) || frames[reference] != phase.InputSource {
		return empty, v2input.ErrInvalid
	}
	raw, err := read(native.Root)
	if err != nil {
		return empty, err
	}
	root, err := v2draft.ParseImageResult(raw, native.Root)
	if err != nil || root.WorkingImage != native.Image || root.Report != native.Report {
		return empty, v2input.ErrInvalid
	}
	if err = validateMultiResultRoot(binding, root); err != nil {
		return empty, err
	}
	sourceRefs := []v2input.Ref{}
	originals := make([]review.WorkingImage, len(frames))
	for i, frame := range frames {
		raw, err = read(frame.Source)
		if err != nil {
			return empty, err
		}
		originals[i], err = review.ParseWorkingImage(raw)
		if err != nil {
			return empty, err
		}
		children, e := v2draft.DocumentChildren(raw, "WorkingImage", frame.Source)
		if e != nil {
			return empty, e
		}
		sourceRefs = append(sourceRefs, frame.Source)
		for _, child := range children {
			sourceRefs = append(sourceRefs, child.Ref)
		}
	}
	image, err := read(native.Image)
	if err != nil {
		return empty, err
	}
	mask, err := read(root.ChangedMask)
	if err != nil {
		return empty, err
	}
	reportRaw, err := read(native.Report)
	if err != nil {
		return empty, err
	}
	report, err := ParseMultiImageReport(reportRaw, binding, native.Report)
	if err != nil {
		return empty, err
	}
	registrations := make([][]byte, len(report.Registrations))
	for i, ref := range report.Registrations {
		registrations[i], err = read(ref)
		if err != nil {
			return empty, err
		}
	}
	target, _, _, err := validateMultiOutputGeometryContent(binding, originals, sourceRefs, image, mask, registrations)
	if err != nil {
		return empty, err
	}
	canvas, err := canonical.Hash(target.Canvas)
	if err != nil {
		return empty, err
	}
	chain, err := canonical.Hash(target.TransformChain)
	if err != nil {
		return empty, err
	}
	return v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}, nil
}

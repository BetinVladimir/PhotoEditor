package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func (s SQLAuthoredNativeReplayCatalog) nativeGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, phase AuthoredPhaseReceipt) (v2draft.DocumentGeometry, error) {
	empty := v2draft.DocumentGeometry{}
	if phase.Geometry == nil {
		return empty, v2input.ErrInvalid
	}
	switch phase.Geometry.Task.Operation {
	case "optical.panorama", "optical.focus_stack":
		return s.nativeMultiGeometry(ctx, who, tenant, phase)
	case "image.upscale":
		return s.nativeUpscaleGeometry(ctx, who, tenant, phase)
	case "optical.lens", "optical.perspective":
	default:
		return empty, v2input.ErrInvalid
	}
	bodies := map[v2input.Ref][]byte{}
	read := func(ref v2input.Ref) ([]byte, error) {
		if raw, ok := bodies[ref]; ok {
			return raw, nil
		}
		raw, err := s.read(ctx, who, tenant, ref)
		if err != nil {
			return nil, err
		}
		bodies[ref] = raw
		return raw, nil
	}
	root, err := read(phase.Geometry.Root)
	if err != nil {
		return empty, err
	}
	parsed, err := v2draft.ParseImageResult(root, phase.Geometry.Root)
	if err != nil || parsed.WorkingImage != phase.Geometry.Image || parsed.Report != phase.Geometry.Report {
		return empty, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{phase.InputSource.Source, parsed.WorkingImage, parsed.ChangedMask, parsed.Report} {
		if _, err = read(ref); err != nil {
			return empty, err
		}
	}
	// Only the typed report's specific perspective transform edge is consumed;
	// no generic JSON traversal or arbitrary embedded reference is authority.
	var report struct {
		Transform *v2input.Ref `json:"transform"`
	}
	if json.Unmarshal(bodies[parsed.Report], &report) != nil {
		return empty, v2input.ErrInvalid
	}
	if report.Transform != nil {
		if _, err = read(*report.Transform); err != nil {
			return empty, err
		}
	}
	return authoredNativeSingleGeometry(phase, bodies)
}

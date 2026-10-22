package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func (s SQLAuthoredNativeReplayCatalog) nativeUpscaleGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, phase AuthoredPhaseReceipt) (v2draft.DocumentGeometry, error) {
	empty := v2draft.DocumentGeometry{}
	if phase.Geometry == nil {
		return empty, v2input.ErrInvalid
	}
	n := phase.Geometry
	refs := []v2input.Ref{n.Task.Params, phase.InputSource.Source, n.Image, n.Report}
	raw := make([][]byte, len(refs))
	for i, ref := range refs {
		var err error
		raw[i], err = s.read(ctx, who, tenant, ref)
		if err != nil {
			return empty, err
		}
	}
	return validateAuthoredUpscaleGeometry(phase, raw[0], raw[1], raw[2], raw[3])
}

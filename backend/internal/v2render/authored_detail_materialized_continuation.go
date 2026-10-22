package v2render

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ResumeMaterialized runs the actual original-region/protection compiler before
// the concrete publication and segment chain. Transform documents must come
// from the protected independent transform producer readback; they are never
// inferred from a face selection or the current full-frame skin mask. The SQL
// publishers authenticate every witness again, so compiler input alone cannot
// confer admission authority.
func (s SQLAuthoredDetailContinuation) ResumeMaterialized(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int,
	styleBody []byte, styleWitnesses map[string]string, originalParams, currentImage []byte, currentRef v2input.Ref,
	documents []AuthoredDetailTransformDocument, transforms json.RawMessage, layerTemplate []byte, maskBindings json.RawMessage, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	params, err := CompileAuthoredDetailOccurrence(originalParams, currentImage, currentRef, documents)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return s.Resume(ctx, who, tenant, parent, ordinal, styleBody, styleWitnesses, params, transforms, layerTemplate, maskBindings, protected)
}

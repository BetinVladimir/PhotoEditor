package semanticreview

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// VerifyRemovalPreviewImageBinding proves that the WorkingImage changes only
// the pixel reference. Physical NPY and producer/fence validation are separate.
func VerifyRemovalPreviewImageBinding(e RemovalPreviewEvidence, sourceRaw, imageRaw []byte) (v2input.SemanticSourceMetadata, error) {
	empty := v2input.SemanticSourceMetadata{}
	if _, _, err := FreezeRemovalPreviewParams(e.Params); err != nil {
		return empty, err
	}
	if len(sourceRaw) < 2 || len(sourceRaw) > 1048576 || len(imageRaw) < 2 || len(imageRaw) > 1048576 ||
		e.SchemaVersion != 1 || e.Algorithm != "big_lama_work_srgb_delta_v1" || !e.ProtectedPixelsUnchanged || !e.OutsideMaskUnchanged {
		return empty, ErrInvalid
	}
	source, err := v2input.ResolveSemanticSourceMetadata(e.Params.Source, sourceRaw)
	if err != nil {
		return empty, ErrInvalid
	}
	if len(e.ROI) != 4 || e.ChangedPixels < 0 || e.ChangedPixels > 200000000 {
		return empty, ErrInvalid
	}
	x0, y0, x1, y1 := e.ROI[0], e.ROI[1], e.ROI[2], e.ROI[3]
	if x0 < 0 || y0 < 0 || x1 <= x0 || y1 <= y0 || x1 > source.Width || y1 > source.Height || e.ChangedPixels > (x1-x0)*(y1-y0) {
		return empty, ErrInvalid
	}
	binding := e.Params.Source
	binding.Source = e.WorkingImage
	output, err := v2input.ResolveSemanticSourceMetadata(binding, imageRaw)
	if err != nil || output.Inputs.Pixels != e.Pixels || output.Inputs.Pixels == source.Inputs.Pixels {
		return empty, ErrInvalid
	}
	var before, after map[string]any
	if json.Unmarshal(sourceRaw, &before) != nil || json.Unmarshal(imageRaw, &after) != nil {
		return empty, ErrInvalid
	}
	delete(before, "pixels")
	delete(after, "pixels")
	if !reflect.DeepEqual(before, after) {
		return empty, ErrInvalid
	}
	return output, nil
}

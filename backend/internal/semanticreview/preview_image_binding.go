package semanticreview

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// VerifyExposurePreviewImageBinding proves that the WorkingImage changes only
// the pixel reference. Physical NPY and producer/fence validation are separate.
func VerifyExposurePreviewImageBinding(e ExposurePreviewEvidence, sourceRaw, imageRaw []byte) (v2input.SemanticSourceMetadata, error) {
	empty := v2input.SemanticSourceMetadata{}
	if _, _, err := FreezeExposurePreviewParams(e.Params); err != nil {
		return empty, err
	}
	if len(sourceRaw) < 2 || len(sourceRaw) > 1048576 || len(imageRaw) < 2 || len(imageRaw) > 1048576 ||
		e.SchemaVersion != 1 || e.Algorithm != "local_exposure_exact_replay_v1" || !e.ProtectedPixelsUnchanged || e.OutsideMaskMaxError != 0 {
		return empty, ErrInvalid
	}
	source, err := v2input.ResolveSemanticSourceMetadata(e.Params.Source, sourceRaw)
	if err != nil {
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

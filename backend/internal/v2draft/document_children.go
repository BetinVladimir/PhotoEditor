package v2draft

import (
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// DocumentChildren validates a supported document before exposing its direct
// typed references. It does not authorize references or validate their bytes.
// Result children follow schema property order, as required by adoption's
// first-visit inventory. Array order is retained by nestedRefs.
func DocumentChildren(raw []byte, contract string, parent v2input.Ref) ([]LayerRef, error) {
	if !isDocumentRef(parent) {
		return nil, v2input.ErrInvalid
	}
	const path = "Document"
	var children []LayerRef
	var err error
	switch contract {
	case "FaceObservations":
		children, err = faceObservationRefs(raw, path, parent)
	case "DescriptorShard":
		children, err = descriptorShardRefs(raw, path, parent)
	case "CandidateProfile":
		_, err = DecodeCandidateProfile(raw)
	case "FrozenRenderRecipe":
		children, err = renderRecipeRefs(raw, path, parent)
	case "ImageResult":
		children, err = imageResultRefs(raw, path, parent)
		// ImageResult has no array-valued child paths.
		sort.Slice(children, func(i, j int) bool { return children[i].Path < children[j].Path })
	case "ImageReport":
		children, err = imageReportRefs(raw, path, parent)
		// Preserve array order within each property.
		sort.SliceStable(children, func(i, j int) bool {
			return reportProperty(children[i].Path) < reportProperty(children[j].Path)
		})
	case "WorkingImage":
		if _, err = review.ParseWorkingImage(raw); err == nil {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
		}
	case "MaskRef":
		if _, err = review.ParseMaskRef(raw); err == nil {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
		}
	default:
		return nil, v2input.ErrInvalid
	}
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	return children, nil
}

func reportProperty(path string) string {
	for i := len("Document."); i < len(path); i++ {
		if path[i] == '[' || path[i] == '.' {
			return path[len("Document."):i]
		}
	}
	return path[len("Document."):]
}

package v2render

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// AdmissionJSON serializes only witnesses issued by original-byte verification.
// SQL must still lock and resolve each exact locator before activating tasks.
func (v VerifiedLensPreviewInputs) AdmissionJSON() ([]byte, error) {
	if err := validateLensPreviewSource(v.source); err != nil {
		return nil, err
	}
	if len(v.arrays) < 1 || len(v.arrays) > 2 {
		return nil, v2input.ErrInvalid
	}
	type witness struct {
		Ref      v2input.Ref `json:"ref"`
		Parent   v2input.Ref `json:"parent"`
		Path     string      `json:"path"`
		Contract string      `json:"contract"`
		Key      string      `json:"object_key"`
		Version  string      `json:"object_version"`
		SHA      string      `json:"sha256"`
		Bytes    int64       `json:"bytes"`
		Media    string      `json:"media_type"`
	}
	values := make([]witness, len(v.arrays))
	for i, a := range v.arrays {
		expected := "WorkingPixels"
		if i == 1 {
			expected = "WorkingAlpha"
		}
		if a.Node.Contract != expected || !validLayerArtifactRef(a.Node.Ref) || !validLayerArtifactRef(a.Node.ParentRef) || a.Object.SHA256 != a.Node.Ref.SHA256 || a.Object.Bytes < 1 || a.Object.Key == "" || a.Object.VersionID == "" || a.Object.VersionID == "null" {
			return nil, v2input.ErrInvalid
		}
		values[i] = witness{a.Node.Ref, a.Node.ParentRef, a.Node.Path, a.Node.Contract, a.Object.Key, a.Object.VersionID, a.Object.SHA256, a.Object.Bytes, a.Object.ContentType}
	}
	return canonical.Marshal(values)
}

package semanticproposal

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Reconciles the exact landmark reference sets across retained catalog, native
// Params and producer packets. Model/content/object authority remains separate.
func validateDefectLandmarkInventory(filesBody, contentBody, facesBody json.RawMessage) error {
	var files []struct {
		Contract string    `json:"contract"`
		ID       uuid.UUID `json:"artifact_id"`
		SHA      string    `json:"sha256"`
	}
	var content defectActivationContent
	var faces []defectFaceAuthorityEntry
	if json.Unmarshal(filesBody, &files) != nil || files == nil || len(files) > 4097 || json.Unmarshal(contentBody, &content) != nil || json.Unmarshal(facesBody, &faces) != nil || faces == nil || len(faces) > 64 {
		return v2input.ErrInvalid
	}
	var params struct {
		Faces []struct {
			Landmarks v2input.Ref `json:"landmarks"`
		} `json:"faces"`
	}
	if json.Unmarshal([]byte(content.ParamsText), &params) != nil || params.Faces == nil || len(params.Faces) > 64 {
		return v2input.ErrInvalid
	}
	required := map[v2input.Ref]bool{}
	ids := map[uuid.UUID]string{}
	for _, face := range params.Faces {
		if face.Landmarks.ArtifactID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if sha, exists := ids[face.Landmarks.ArtifactID]; exists && sha != face.Landmarks.SHA256 {
			return v2input.ErrInvalid
		}
		ids[face.Landmarks.ArtifactID] = face.Landmarks.SHA256
		required[face.Landmarks] = true
	}
	catalog := map[v2input.Ref]bool{}
	for _, file := range files {
		if file.Contract != "FaceLandmarks" {
			continue
		}
		ref := v2input.Ref{ArtifactID: file.ID, SHA256: file.SHA}
		if !required[ref] || catalog[ref] {
			return v2input.ErrInvalid
		}
		catalog[ref] = true
	}
	if len(catalog) != len(required) || len(faces) != len(required) {
		return v2input.ErrInvalid
	}
	for _, face := range faces {
		if !catalog[face.Landmarks] {
			return v2input.ErrInvalid
		}
		delete(catalog, face.Landmarks)
	}
	if len(catalog) != 0 {
		return v2input.ErrInvalid
	}
	return nil
}

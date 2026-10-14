package semanticproposal

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestDefectLandmarkInventoryRequiresExactProducerSetForAllFaces(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	files, _ := json.Marshal([]map[string]any{{"contract": "FaceLandmarks", "artifact_id": ref.ArtifactID, "sha256": ref.SHA256}})
	params, _ := json.Marshal(map[string]any{"faces": []map[string]any{{"landmarks": ref}, {"landmarks": ref}}})
	content, _ := json.Marshal(defectActivationContent{ParamsText: string(params)})
	faces := []defectFaceAuthorityEntry{{Landmarks: ref}}
	faceBody, _ := json.Marshal(faces)
	if err := validateDefectLandmarkInventory(files, content, faceBody); err != nil {
		t.Fatal("shared landmark producer rejected", err)
	}
	for changed := 0; changed < 4; changed++ {
		candidate := append([]defectFaceAuthorityEntry(nil), faces...)
		switch changed {
		case 0:
			candidate = []defectFaceAuthorityEntry{}
		case 1:
			candidate[0].Landmarks.ArtifactID = uuid.New()
		case 2:
			candidate[0].Landmarks.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		case 3:
			candidate = append(candidate, candidate[0])
		}
		candidateBody, _ := json.Marshal(candidate)
		if err := validateDefectLandmarkInventory(files, content, candidateBody); err != v2input.ErrInvalid {
			t.Fatal("changed landmark inventory accepted", changed, err)
		}
	}
}

func TestDefectLandmarkInventoryRejectsMissingCatalogAndAllowsExplicitEmptyFaces(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	params, _ := json.Marshal(map[string]any{"faces": []map[string]any{{"landmarks": ref}}})
	content, _ := json.Marshal(defectActivationContent{ParamsText: string(params)})
	faces, _ := json.Marshal([]defectFaceAuthorityEntry{{Landmarks: ref}})
	if err := validateDefectLandmarkInventory([]byte("[]"), content, faces); err != v2input.ErrInvalid {
		t.Fatal("missing catalog landmark accepted", err)
	}
	unrelated, _ := json.Marshal([]map[string]any{{"contract": "FaceLandmarks", "artifact_id": uuid.New(), "sha256": ref.SHA256}})
	if err := validateDefectLandmarkInventory(unrelated, content, faces); err != v2input.ErrInvalid {
		t.Fatal("unrelated catalog landmark accepted", err)
	}
	emptyContent, _ := json.Marshal(defectActivationContent{ParamsText: `{"faces":[]}`})
	if err := validateDefectLandmarkInventory([]byte("[]"), emptyContent, []byte("[]")); err != nil {
		t.Fatal("explicit empty inventory rejected", err)
	}
	for _, pair := range [][2]json.RawMessage{{[]byte("null"), []byte("[]")}, {[]byte("[]"), []byte("null")}} {
		if err := validateDefectLandmarkInventory(pair[0], emptyContent, pair[1]); err != v2input.ErrInvalid {
			t.Fatal("null inventory accepted", err)
		}
	}
}

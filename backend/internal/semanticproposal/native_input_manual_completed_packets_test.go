package semanticproposal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Packet correspondence only; synthetic catalog pins are not SQL authority.
func TestNativeManualCompletedPacketsRejectChangedCatalogAndFiles(t *testing.T) {
	reader := SQLTransferSourceReader{Bucket: "test"}
	proof := nativeInputManualDependencies{}
	proof.selection.landmarks.catalog.ResultID = uuid.New()
	proof.selection.skin.catalog.ResultID = uuid.New()
	proof.selection.landmarks.catalog.Outputs = []v2input.Ref{{ArtifactID: uuid.New()}}
	proof.selection.skin.catalog.Outputs = []v2input.Ref{{ArtifactID: uuid.New()}}
	proof.selection.skin.skinPixels = 7
	packet := func(result uuid.UUID, outputs []v2input.Ref, objects []defectEvidenceObject, roles []string, skin bool) string {
		files := make([]nativeSkinCompletedFile, len(objects))
		for i, object := range objects {
			files[i] = nativeSkinCompletedFile{Role: roles[i], Ref: object.ref, Bucket: reader.Bucket, Key: object.object.Key, Version: object.object.VersionID, Bytes: object.object.Bytes, Media: object.object.ContentType}
		}
		body := map[string]any{"schema_version": 1, "admission": map[string]any{"original": true}, "result_id": result, "outputs": outputs, "params": v2input.Ref{}, "files": files}
		if skin {
			body["skin_pixels"] = int64(7)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	proof.landmarks = packet(proof.selection.landmarks.catalog.ResultID, proof.selection.landmarks.catalog.Outputs, proof.selection.landmarks.objects[:], []string{"face_landmarks", "params"}, false)
	proof.skin = packet(proof.selection.skin.catalog.ResultID, proof.selection.skin.catalog.Outputs, proof.selection.skin.objects[:], []string{"skin_mask", "skin_mask_png", "skin_mask_ref", "params"}, true)
	if err := reader.verifyNativeInputManualCompletedPackets(context.Background(), proof); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"result", "params", "file_version", "pixels", "unknown", "trailing", "admission"} {
		changed := proof
		switch field {
		case "result":
			changed.selection.landmarks.catalog.ResultID = uuid.New()
		case "params":
			changed.selection.skin.catalog.Params.ArtifactID = uuid.New()
		case "file_version":
			changed.selection.skin.objects[0].object.VersionID = "changed"
		case "pixels":
			changed.selection.skin.skinPixels++
		case "unknown":
			changed.skin = `{"unknown":true,` + proof.skin[1:]
		case "trailing":
			changed.skin += " {}"
		case "admission":
			var body map[string]any
			if json.Unmarshal([]byte(proof.skin), &body) != nil {
				t.Fatal("fixture")
			}
			body["admission"] = nil
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			changed.skin = string(raw)
		}
		if err := reader.verifyNativeInputManualCompletedPackets(context.Background(), changed); err == nil {
			t.Fatal("changed packet accepted", field)
		}
	}
}

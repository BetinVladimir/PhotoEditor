package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

func TestRenderDerivativeDocumentsBindCanonicalRootAndSource(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{}
	for _, scenario := range []struct{ alpha, large bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		alpha := scenario.alpha
		var image map[string]any
		if err = json.Unmarshal(fixtures[0], &image); err != nil {
			t.Fatal(err)
		}
		ref := func() map[string]any {
			return map[string]any{"artifact_id": uuid.New(), "sha256": strings.Repeat("a", 64)}
		}
		if alpha {
			image["alpha"] = ref()
		}
		if scenario.large {
			canvas := image["canvas"].(map[string]any)
			canvas["width"] = 6000
			canvas["height"] = 4000
		}
		imageBytes, err := canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		imageRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(imageBytes)}
		resultBytes, _ := canonical.Marshal(map[string]any{"schema_version": 1, "working_image": imageRef, "changed_mask": ref(), "report": ref(), "generated_mask": nil})
		intent := v2render.RenderDerivativeIntent{Run: uuid.New(), Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(resultBytes)}, SRGBSHA: strings.Repeat("b", 64)}
		docs, err := BuildRenderDerivativeDocuments(intent, resultBytes, imageBytes)
		if err != nil || len(docs) != 2 {
			t.Fatal(err)
		}
		cases = append(cases, map[string]any{"root_ref": intent.Root, "root": json.RawMessage(resultBytes), "image": json.RawMessage(imageBytes), "srgb_sha": intent.SRGBSHA, "documents": docs})
		for _, doc := range docs {
			var params struct {
				Image      review.ArtifactRef `json:"image"`
				SourceKind string             `json:"source_kind"`
				Variant    string             `json:"variant"`
			}
			if err = json.Unmarshal(doc.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.Image.ID != imageRef.ArtifactID || params.Variant != doc.Variant || params.SourceKind != "working" || renderDerivativeBytesSHA(doc.Params) != doc.ParamsSHA {
				t.Fatal("wrong presentation binding")
			}
			count := 2
			if alpha {
				count = 3
			}
			if len(doc.Inputs) != count {
				t.Fatal("lost alpha")
			}
			paramsRef := review.ArtifactRef{ID: uuid.New(), SHA256: doc.ParamsSHA}
			task, err := PrepareRenderDerivativeTask(doc, paramsRef)
			if err != nil || task.Operation != "media.preview" || task.Params != paramsRef || task.TimeoutSeconds != 300 || task.ID != doc.ID {
				t.Fatal("invalid presentation task", err)
			}
			original := doc.Inputs[0]
			doc.Inputs[0] = review.ArtifactRef{ID: uuid.New(), SHA256: original.SHA256}
			if task.Inputs[0] != original {
				t.Fatal("task borrows mutable inputs")
			}
			if _, err = PrepareRenderDerivativeTask(doc, paramsRef); err == nil {
				t.Fatal("rebound source accepted")
			}
			doc.Inputs[0] = original
			badProfile := doc
			badProfile.Profile = []byte(strings.Replace(string(doc.Profile), "cpu", "cuda", 1))
			if _, err = PrepareRenderDerivativeTask(badProfile, paramsRef); err == nil {
				t.Fatal("unqualified backend accepted")
			}
			wrong := paramsRef
			wrong.SHA256 = strings.Repeat("f", 64)
			if _, err = PrepareRenderDerivativeTask(doc, wrong); err == nil {
				t.Fatal("uploaded parameter digest replacement accepted")
			}
			if _, err = ParseResourceEstimate(doc.Profile); err != nil {
				t.Fatal(err)
			}
		}
		repeat, err := BuildRenderDerivativeDocuments(intent, resultBytes, imageBytes)
		if err != nil || repeat[0].ID != docs[0].ID || string(repeat[0].Params) != string(docs[0].Params) {
			t.Fatal("unstable plan", err)
		}
		if _, err = BuildRenderDerivativeDocuments(intent, append(append([]byte(nil), resultBytes...), ' '), imageBytes); err == nil {
			t.Fatal("noncanonical root accepted")
		}
		if _, err = BuildRenderDerivativeDocuments(intent, resultBytes, append(append([]byte(nil), imageBytes...), ' ')); err == nil {
			t.Fatal("noncanonical image accepted")
		}
		changed := intent
		changed.Root.SHA256 = strings.Repeat("c", 64)
		if _, err = BuildRenderDerivativeDocuments(changed, resultBytes, imageBytes); err == nil {
			t.Fatal("root digest replacement accepted")
		}
	}
	if path := os.Getenv("PHOTO_RENDER_DERIVATIVE_WIRE"); path != "" {
		body, err := canonical.Marshal(cases)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}

}

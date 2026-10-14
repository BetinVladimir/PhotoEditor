package v2render

import (
	"errors"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRenderClosureDocumentsRejectContradictoryReadEvidence(t *testing.T) {
	_, _, layers := layerDocumentFixture(t)
	for _, mutation := range []string{"unchanged", "version", "media", "size", "key", "path", "ref", "body", "contract", "inventory"} {
		t.Run(mutation, func(t *testing.T) {
			layer := layers[0]
			document := v2draft.DocumentWitness{Path: layer.Path, Contract: layer.Contract, Ref: layer.Ref, Parent: layer.Parent, JCS: layer.JCS,
				Object: objectstore.Object{Key: "params", VersionID: "v1", SHA256: layer.Ref.SHA256, ContentType: "application/json", Bytes: int64(len(layer.JCS))}}
			entries := []v2input.Entry{{Ref: layer.Ref, Contract: layer.Contract}}
			switch mutation {
			case "version":
				document.Object.VersionID = "null"
			case "media":
				document.Object.ContentType = "application/octet-stream"
			case "size":
				document.Object.Bytes++
			case "key":
				document.Object.Key = ""
			case "path":
				document.Path = ""
			case "ref":
				document.Ref.SHA256 = "bad"
			case "body":
				document.JCS = "{}"
				document.Object.Bytes = 2
			case "contract":
				document.Contract = "ImageResult"
			case "inventory":
				entries = []v2input.Entry{}
			}
			err := validateRenderClosureDocuments([]v2draft.DocumentWitness{document}, entries)
			if mutation == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("contradictory read evidence accepted", err)
			}
		})
	}
}

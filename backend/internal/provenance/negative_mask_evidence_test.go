package provenance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestSigningNegativeMaskRequiresExactImageGeometryAndPurpose(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(fixture, &images); err != nil || len(images) == 0 {
		t.Fatal("missing image fixture", err)
	}
	rawImage, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(rawImage)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"changed", "generated", "selector", "protected", "object", "confidence", "model", "canvas", "hash", "mutation", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			selector := "changed"
			if mode == "generated" {
				selector = "generated"
			}
			mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: selector, Confidence: 1}
			switch mode {
			case "selector":
				mask.Selector = "person"
			case "protected":
				mask.Protected = true
			case "object":
				id := uuid.New()
				mask.ObjectID = &id
			case "confidence":
				mask.Confidence = .5
			case "model":
				sha := strings.Repeat("b", 64)
				mask.ModelLockID = &sha
			case "canvas":
				mask.Canvas.Width++
			}
			raw, reference := contractBytes(t, mask)
			if mode == "hash" {
				reference.SHA256 = strings.Repeat("0", 64)
			}
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "optical.negative", Version: "1", InputDigest: strings.Repeat("c", 64), ResultDigest: strings.Repeat("d", 64), Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}, Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			reads := 0
			documents := signingDocumentsFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) ([]byte, error) {
				reads++
				if mode == "mutation" && reads == 2 {
					return []byte(`{}`), nil
				}
				return raw, nil
			})
			actual, err := ReadSigningNegativeMask(ctx, documents, who, ready, task, image, reference, selector)
			if mode == "changed" {
				if err != nil || reads != 2 || actual.Selector != selector {
					t.Fatal("valid mask refused", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid render mask accepted")
			}
			if mode == "cancelled" && reads != 0 {
				t.Fatal("cancelled mask performed IO")
			}
		})
	}
}

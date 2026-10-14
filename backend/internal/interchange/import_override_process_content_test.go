package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestProcessOverrideEnvelopeBindsCurrentSourceAndCanvas(t *testing.T) {
	_, _, master := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(master)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(master)}, CanvasHash: canvas}
	body, err := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256, "modules": map[string]any{"strength": 0.7}, "revision": 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "binding", "source", "canvas", "document"} {
		t.Run(mode, func(t *testing.T) {
			reader := &importMasterReaderStub{body: master}
			policy := ImportProcessOverrideEnvelope{Masters: reader, Binding: binding}
			actual := binding
			owned := append([]byte(nil), body...)
			switch mode {
			case "binding":
				actual.AssetVersionID = uuid.New()
			case "source":
				reader.body = []byte(`{}`)
			case "canvas":
				policy.Binding.CanvasHash = rawSHA([]byte("other canvas"))
				actual = policy.Binding
			case "document":
				owned = []byte(`{"schema_version":1}`)
			}
			err := policy.QualifyImportOverrideContent(context.Background(), owned, actual)
			if (err == nil) != (mode == "valid") {
				t.Fatal("unexpected process content result", mode, err)
			}
			if mode == "binding" && reader.calls != 0 {
				t.Fatal("foreign binding read master")
			}
		})
	}
}

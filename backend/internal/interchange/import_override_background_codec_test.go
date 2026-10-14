package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type overrideBackgroundStub struct {
	called int
	result string
}

type overrideArchiveStub struct{ called int }

func (s *overrideArchiveStub) ValidateArchivedImportOverride(_ context.Context, raw []byte) error {
	s.called++
	if len(raw) < 2 {
		return v2input.ErrInvalid
	}
	return nil
}

func (s *overrideBackgroundStub) RemapImportedOverrideBackground(_ context.Context, ref v2input.Ref, portable string, target v2input.SessionVersionBinding) (string, error) {
	s.called++
	if !validImportRef(ref) || portable != "foreign-asset" || target.AssetVersionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	return s.result, nil
}

func TestOverrideBackgroundRequiresDistinctQualifiedRemap(t *testing.T) {
	_, master, imageRaw := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: master.Ref, CanvasHash: canvas}
	target := old
	target.AssetVersionID = uuid.New()
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256,
		"modules": map[string]any{"extensions": map[string]any{"background": map[string]any{
			"mode": "asset", "asset_id": "foreign-asset", "blur_strength": 0, "protect_subject": true,
		}}}, "revision": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	archived := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
	qualifier := &overrideQualifierStub{}
	codec := (ImportOverrideCodec{Masters: &importMasterReaderStub{body: imageRaw}, Qualifier: qualifier}).BindImportDocumentSource(archived).(ImportOverrideCodec)
	resolve := func(v2input.Ref, string) (v2input.Ref, error) {
		t.Fatal("unexpected dependency publication")
		return v2input.Ref{}, nil
	}
	if prepared, err := codec.RemapImportDocument(context.Background(), "Override", raw, old, target, resolve); err == nil || prepared != nil || qualifier.calls != 0 {
		t.Fatal("foreign background asset escaped without remapper", err)
	}
	remapper := &overrideBackgroundStub{result: "local-asset"}
	codec.Backgrounds = remapper
	if prepared, err := codec.RemapImportDocument(context.Background(), "Override", raw, old, target, resolve); err == nil || prepared != nil || remapper.called != 0 {
		t.Fatal("asset publication reached without archive qualification", err)
	}
	archive := &overrideArchiveStub{}
	codec.Archive = archive
	prepared, err := codec.RemapImportDocument(context.Background(), "Override", raw, old, target, resolve)
	if err != nil || archive.called != 1 || remapper.called != 1 || qualifier.calls != 1 {
		t.Fatal("qualified background remap failed", err)
	}
	var document struct {
		Modules struct {
			Extensions struct {
				Background struct {
					AssetID string `json:"asset_id"`
				} `json:"background"`
			} `json:"extensions"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(prepared, &document); err != nil || document.Modules.Extensions.Background.AssetID != "local-asset" {
		t.Fatal("foreign portable asset was retained", err)
	}
	remapper.result = "foreign-asset"
	if prepared, err := codec.RemapImportDocument(context.Background(), "Override", raw, old, target, resolve); err == nil || prepared != nil || qualifier.calls != 1 {
		t.Fatal("unchanged foreign portable asset escaped", err)
	}
}

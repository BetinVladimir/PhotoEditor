package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type resolvedQualifierStub struct {
	archived, imported int
	fail               error
}

func (s *resolvedQualifierStub) ValidateArchivedImportResolved(_ context.Context, _ []byte, _ v2input.SessionVersionBinding) error {
	s.archived++
	return s.fail
}
func (s *resolvedQualifierStub) QualifyImportedResolved(_ context.Context, raw []byte, _ v2input.SessionVersionBinding) error {
	s.imported++
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	var got string
	if err := json.Unmarshal(body["hash"], &got); err != nil {
		return err
	}
	delete(body, "hash")
	want, err := canonical.Hash(body)
	if err != nil {
		return err
	}
	if got != want {
		return v2input.ErrInvalid
	}
	return s.fail
}

func TestImportResolvedCodecRebindsTypedChildrenAndBodyHash(t *testing.T) {
	oldImage := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("old image"))}
	newImage := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("new image"))}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
	stats := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("stats"))}
	lut := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}
	old := v2input.SessionVersionBinding{Source: oldImage, AssetVersionID: uuid.New(), CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{Source: newImage, AssetVersionID: uuid.New(), CanvasHash: old.CanvasHash}
	body := map[string]any{"params": map[string]any{"image": oldImage, "style": style, "stats": stats}, "absolute_lut": map[string]any{"artifact": lut, "size": 33}, "mode": "adaptive"}
	oldHash, err := canonical.Hash(body)
	if err != nil {
		t.Fatal(err)
	}
	body["hash"] = oldHash
	raw, err := canonical.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	qualifier := &resolvedQualifierStub{}
	codec := ImportResolvedCodec{Qualifier: qualifier}
	seen := []string{}
	resolve := func(ref v2input.Ref, kind string) (v2input.Ref, error) {
		seen = append(seen, kind)
		if (kind == "StyleV2" && ref != style) || (kind == "Stats20" && ref != stats) || (kind == "AbsoluteLUTArray" && ref != lut) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, nil
	}
	prepared, err := codec.RemapImportDocument(context.Background(), "ResolvedStyle", raw, old, target, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if qualifier.archived != 1 || qualifier.imported != 1 || len(seen) != 3 || seen[0] != "StyleV2" || seen[1] != "Stats20" || seen[2] != "AbsoluteLUTArray" {
		t.Fatal("incomplete typed remap", seen)
	}
	var result struct {
		Params struct {
			Image v2input.Ref `json:"image"`
			Style v2input.Ref `json:"style"`
			Stats v2input.Ref `json:"stats"`
		} `json:"params"`
		AbsoluteLUT struct {
			Artifact v2input.Ref `json:"artifact"`
		} `json:"absolute_lut"`
	}
	if err := json.Unmarshal(prepared, &result); err != nil {
		t.Fatal(err)
	}
	if result.Params.Image != newImage || result.Params.Style.ArtifactID == style.ArtifactID || result.Params.Stats.ArtifactID == stats.ArtifactID || result.AbsoluteLUT.Artifact.ArtifactID == lut.ArtifactID {
		t.Fatal("archived dependency retained")
	}
	if string(prepared) == string(raw) {
		t.Fatal("archived hash retained")
	}
}

func TestImportResolvedCodecStopsBeforeChildrenWhenArchivedQualificationFails(t *testing.T) {
	sentinel := errors.New("runtime pin unavailable")
	qualifier := &resolvedQualifierStub{fail: sentinel}
	codec := ImportResolvedCodec{Qualifier: qualifier}
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	binding := v2input.SessionVersionBinding{Source: image, AssetVersionID: uuid.New()}
	called := false
	_, err := codec.RemapImportDocument(context.Background(), "ResolvedStyle", []byte(`{"hash":"invalid","params":{}}`), binding, binding, func(v2input.Ref, string) (v2input.Ref, error) { called = true; return v2input.Ref{}, nil })
	if !errors.Is(err, sentinel) || called || qualifier.imported != 0 {
		t.Fatal("archived qualification bypassed", err)
	}
}

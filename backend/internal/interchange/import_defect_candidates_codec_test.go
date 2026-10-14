package interchange

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type importDefectCandidatesQualifierStub struct{ archived, current int }

func (s *importDefectCandidatesQualifierStub) ValidateArchivedImportDefectCandidates(_ context.Context, _ []byte, _ v2input.SessionVersionBinding) error {
	s.archived++
	return nil
}
func (s *importDefectCandidatesQualifierStub) QualifyImportedDefectCandidates(_ context.Context, _ []byte, _ v2input.SessionVersionBinding) error {
	s.current++
	return nil
}

func TestImportDefectCandidatesRehashesForTargetPixels(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Image          v2input.Ref     `json:"image"`
		SourcePixels   v2input.Ref     `json:"source_pixels"`
		LabelMap       v2input.Ref     `json:"label_map"`
		Canvas         review.Canvas   `json:"canvas"`
		TransformChain json.RawMessage `json:"transform_chain"`
		Candidates     []struct {
			ID string `json:"id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("target pixels"))}
	imageRaw, err := canonical.Marshal(review.WorkingImage{Pixels: pixels, Canvas: doc.Canvas, Color: review.ColorSpace{ProfileID: "linear_srgb_v1", ICCSHA256: rawSHA([]byte("icc")), Encoding: "linear"}, DType: "float32", TransformChain: doc.TransformChain})
	if err != nil {
		t.Fatal(err)
	}
	canvasHash, err := canonical.Hash(doc.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: doc.Image, CanvasHash: canvasHash}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(imageRaw)}, CanvasHash: canvasHash}
	qualifier := &importDefectCandidatesQualifierStub{}
	codec := ImportDefectCandidatesCodec{Masters: &importMasterReaderStub{body: imageRaw}, Qualifier: qualifier}
	if _, err := (ImportDefectCandidatesCodec{Masters: codec.Masters}).RemapImportDocument(context.Background(), "DefectCandidates", raw, old, target, nil); err == nil {
		t.Fatal("missing provenance authority accepted")
	}
	calls := 0
	prepared, err := codec.RemapImportDocument(context.Background(), "DefectCandidates", raw, old, target, func(source v2input.Ref, kind string) (v2input.Ref, error) {
		calls++
		if kind != "DefectLabelMap" || source != doc.LabelMap {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: source.SHA256}, nil
	})
	if err != nil || calls != 1 || qualifier.archived != 1 || qualifier.current != 1 {
		t.Fatal("typed candidate remap failed", err)
	}
	var changed struct {
		Image        v2input.Ref `json:"image"`
		SourcePixels v2input.Ref `json:"source_pixels"`
		Candidates   []struct {
			ID string `json:"id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(prepared, &changed); err != nil || changed.Image != target.Source || changed.SourcePixels.ArtifactID != pixels.ID || len(changed.Candidates) != len(doc.Candidates) || changed.Candidates[0].ID == doc.Candidates[0].ID {
		t.Fatal("target candidate identity was not rebuilt", err)
	}
}

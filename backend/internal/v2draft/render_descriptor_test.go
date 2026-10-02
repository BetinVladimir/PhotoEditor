package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDescriptorShardTypedRefsAndDuplicateOrdinal(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	face := ref()
	parent := ref()
	frame := map[string]any{"schema_version": 1, "binding": binding, "ordinal": 0, "captured_at": nil, "camera_hash": nil, "scene_embedding": make([]float64, 384), "luma_quantiles": []float64{.1, .2, .3, .4, .5}, "neutral_rgb": nil, "sharpness": 1, "clipping_fraction": 0, "person_fraction": 0, "faces": face}
	body := map[string]any{"schema_version": 1, "snapshot_digest": strings.Repeat("c", 64), "frames": []any{frame}}
	raw, _ := canonical.Marshal(body)
	refs, err := DocumentChildren(raw, "DescriptorShard", parent)
	if err != nil || len(refs) != 2 || refs[0].Ref != binding.Source || refs[0].CanvasHash != binding.CanvasHash || refs[1].Contract != "FaceObservations" || refs[1].Ref != face || refs[1].ExpectedBinding == nil || *refs[1].ExpectedBinding != binding {
		t.Fatal(refs, err)
	}
	body["frames"] = []any{frame, frame}
	raw, _ = canonical.Marshal(body)
	if _, err := DocumentChildren(raw, "DescriptorShard", parent); err == nil {
		t.Fatal("duplicate ordinal accepted")
	}
}

func TestPreEditFaceDocumentSourceAndMalformedFace(t *testing.T) {
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: parent, CanvasHash: strings.Repeat("b", 64)}
	body := map[string]any{"schema_version": 1, "binding": binding, "faces": []any{}}
	raw, _ := canonical.Marshal(body)
	refs, err := DocumentChildren(raw, "FaceObservations", parent)
	if err != nil || len(refs) != 1 || refs[0].Ref != binding.Source || refs[0].CanvasHash != binding.CanvasHash {
		t.Fatal(refs, err)
	}
	body["faces"] = []any{map[string]any{"face_id": uuid.New()}}
	raw, _ = canonical.Marshal(body)
	if _, err := DocumentChildren(raw, "FaceObservations", parent); err == nil {
		t.Fatal("malformed face accepted")
	}
}

func TestFaceOccurrenceRejectsDifferentFrameBinding(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	raw, _ := canonical.Marshal(map[string]any{"binding": binding})
	node := LayerRef{ExpectedBinding: &binding}
	if validateFaceOccurrence(raw, node) != nil {
		t.Fatal("valid binding rejected")
	}
	other := binding
	other.AssetVersionID = uuid.New()
	raw, _ = canonical.Marshal(map[string]any{"binding": other})
	if validateFaceOccurrence(raw, node) == nil {
		t.Fatal("different frame accepted")
	}
}

package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestPreparedLUTWitnessRequiresQualifiedParentEdge(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	lut := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: rawSHA([]byte("canvas"))}
	raw, err := canonical.Marshal(map[string]any{"params": map[string]any{"image": source}, "absolute_lut": map[string]any{"artifact": lut, "size": 33, "dtype": "float32", "order": "RGB_C", "profile_id": "work_rec2020_gamma24_v1"}})
	if err != nil {
		t.Fatal(err)
	}
	parent := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}, Contract: "ResolvedStyle"}
	children, err := exportResolvedChildren(parent, raw)
	if err != nil {
		t.Fatal(err)
	}
	witnesses := &ImportPreparedLUTWitnesses{}
	if err := witnesses.Remember(context.Background(), parent, raw, binding, children); err != nil {
		t.Fatal(err)
	}
	found := witnesses.lookup(lut)
	if len(found) != 1 || found[0].Parent != parent.Ref || found[0].Geometry.Width != 33 {
		t.Fatal("LUT witness not retained", found)
	}
	withoutLUT := []v2input.Entry{}
	for _, child := range children {
		if child.Contract != "AbsoluteLUTArray" {
			withoutLUT = append(withoutLUT, child)
		}
	}
	if err := witnesses.Remember(context.Background(), parent, raw, binding, withoutLUT); err == nil {
		t.Fatal("missing LUT edge accepted")
	}
	foreign := binding
	foreign.Source.ArtifactID = uuid.New()
	if err := witnesses.Remember(context.Background(), parent, raw, foreign, children); err == nil {
		t.Fatal("foreign source accepted")
	}
}

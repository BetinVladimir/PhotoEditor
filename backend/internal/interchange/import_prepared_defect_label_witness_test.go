package interchange

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPreparedDefectLabelWitnessRequiresTypedParentEdge(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var refs struct {
		Image  v2input.Ref `json:"image"`
		Labels v2input.Ref `json:"label_map"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatal(err)
	}
	parent := v2input.Entry{Contract: "DefectCandidates", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}}
	label := v2input.Entry{Contract: "DefectLabelMap", Ref: refs.Labels}
	w := &ImportPreparedDefectLabelWitnesses{}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: refs.Image}
	if err := w.Remember(context.Background(), parent, raw, binding, []v2input.Entry{label}); err != nil {
		t.Fatal(err)
	}
	items := w.lookup(label.Ref)
	if len(items) != 1 || items[0].Parent != parent.Ref || items[0].Width != 400 || items[0].Height != 300 {
		t.Fatal("label witness omitted", items)
	}
	if err := w.Remember(context.Background(), parent, raw, binding, []v2input.Entry{{Contract: "MaskPixels", Ref: label.Ref}}); err == nil {
		t.Fatal("wrong child role accepted")
	}
	wrong := binding
	wrong.Source.ArtifactID = uuid.New()
	if err := w.Remember(context.Background(), parent, raw, wrong, []v2input.Entry{label}); err == nil {
		t.Fatal("foreign source accepted")
	}
}

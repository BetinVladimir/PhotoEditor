package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRenderDescriptorOccurrenceRequiresSelectedOrdinalSource(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: strings.Repeat("b", 64)}
	learning := FrozenRenderLearning{VersionID: uuid.New(), Model: ref, Descriptor: ref, AnalysisID: uuid.New(), Ordinal: 7}
	node := LayerRef{Ref: ref, Contract: "DescriptorShard", ExpectedBinding: &binding, ExpectedLearning: &learning}
	frame := func(ordinal int, b v2input.SessionVersionBinding) any {
		return map[string]any{"ordinal": ordinal, "binding": b}
	}
	wrong := binding
	wrong.AssetVersionID = uuid.New()
	for i, frames := range [][]any{{frame(7, binding)}, {frame(6, binding)}, {frame(7, wrong), frame(8, binding)}, {frame(7, binding), frame(7, binding)}} {
		body, _ := canonical.Marshal(map[string]any{"frames": frames})
		err := validateRenderDescriptorOccurrence(body, node)
		if (i == 0) != (err == nil) {
			t.Fatal(i, err)
		}
	}
}

package v2input

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestDiffCorrectionParameters(t *testing.T) {
	binding := SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     Ref{uuid.New(), strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	base := sessionRecipe{SchemaVersion: 3, Source: binding,
		BaseStyle: Ref{uuid.New(), strings.Repeat("c", 64)},
		Overrides: []Ref{}, RendererVersion: "v2.2"}
	encode := func(recipe sessionRecipe) []byte {
		body, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	approved := base
	approved.ParameterDelta.EV = 0.2
	approved.ParameterDelta.LogB = 0.1
	paths, err := DiffCorrectionParameters(encode(base), encode(approved), binding)
	if err != nil || !reflect.DeepEqual(paths, []string{"/ev", "/log_b"}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	if _, err := DiffCorrectionParameters(encode(base), encode(base), binding); err == nil {
		t.Fatal("empty correction accepted")
	}
	approved.BaseStyle = Ref{uuid.New(), strings.Repeat("d", 64)}
	if _, err := DiffCorrectionParameters(encode(base), encode(approved), binding); err == nil {
		t.Fatal("changed style disguised as scalar correction")
	}
	approved.BaseStyle = base.BaseStyle
	approved.LayerStack = &Ref{uuid.New(), strings.Repeat("e", 64)}
	if _, err := DiffCorrectionParameters(encode(base), encode(approved), binding); err == nil {
		t.Fatal("changed layers accepted without semantic diff")
	}
	approved.LayerStack = nil
	approved.ProtectedMask = &Ref{uuid.New(), strings.Repeat("f", 64)}
	if _, err := DiffCorrectionParameters(encode(base), encode(approved), binding); err == nil {
		t.Fatal("changed protected mask accepted as scalar correction")
	}
	approved.ProtectedMask = nil
	approved.Source.AssetVersionID = uuid.New()
	if _, err := DiffCorrectionParameters(encode(base), encode(approved), binding); err == nil {
		t.Fatal("cross-source correction accepted")
	}
}

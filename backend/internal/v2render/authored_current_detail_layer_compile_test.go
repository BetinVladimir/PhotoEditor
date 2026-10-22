package v2render

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

//go:embed testdata/authored-current-detail-layer-python.json
var authoredCurrentDetailLayerPython []byte

type authoredCurrentDetailLayerVector struct {
	Source   AuthoredInstructionSource          `json:"source"`
	Params   v2input.Ref                        `json:"params"`
	Bindings []AuthoredCurrentDetailMaskBinding `json:"bindings"`
	Expected string                             `json:"expected"`
}

func currentDetailLayerFixture(t *testing.T) authoredCurrentDetailLayerVector {
	t.Helper()
	var v authoredCurrentDetailLayerVector
	if err := json.Unmarshal(authoredCurrentDetailLayerPython, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestAuthoredCurrentDetailLayersMatchesActualPythonFiveOperations(t *testing.T) {
	v := currentDetailLayerFixture(t)
	before, err := canonical.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CompileAuthoredCurrentDetailLayers(context.Background(), v.Source, v.Params, v.Bindings)
	if err != nil || !bytes.Equal(got, []byte(v.Expected)) {
		t.Fatalf("Python mixed-mask compilation mismatch: %v\n%s", err, got)
	}
	after, err := canonical.Marshal(v)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source or binding inputs changed")
	}
	var stack struct {
		Layers []map[string]json.RawMessage `json:"layers"`
	}
	if json.Unmarshal(got, &stack) != nil || len(stack.Layers) != 1 {
		t.Fatal("not a singleton")
	}
	var original struct {
		Layers []map[string]json.RawMessage `json:"layers"`
	}
	if json.Unmarshal([]byte(v.Source.LayerStackJCS), &original) != nil {
		t.Fatal("bad fixture")
	}
	for key, value := range original.Layers[0] {
		if key != "params" && key != "mask" && !bytes.Equal(value, stack.Layers[0][key]) {
			t.Fatalf("original setting changed: %s", key)
		}
	}
}
func TestAuthoredCurrentDetailLayersRefusesForeignIncompleteAndUnusedBindings(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "unused", "invalid_source", "invalid_current", "foreign_asset", "foreign_occurrence", "foreign_operation", "disabled", "transparent", "noncanonical", "invalid_params"} {
		t.Run(kind, func(t *testing.T) {
			v := currentDetailLayerFixture(t)
			switch kind {
			case "missing":
				v.Bindings = nil
			case "duplicate":
				v.Bindings = append(v.Bindings, v.Bindings[0])
			case "unused":
				extra := v.Bindings[0]
				extra.Source.ArtifactID = uuid.New()
				v.Bindings = append(v.Bindings, extra)
			case "invalid_source":
				v.Bindings[0].Source = v2input.Ref{}
			case "invalid_current":
				v.Bindings[0].Current = v2input.Ref{}
			case "foreign_asset":
				v.Source.Source.AssetVersionID = uuid.New()
			case "foreign_occurrence":
				v.Source.Instruction.Layers[0] = uuid.New()
			case "invalid_params":
				v.Params = v2input.Ref{}
			case "noncanonical":
				v.Source.LayerStackJCS = " " + v.Source.LayerStackJCS
			default:
				var stack map[string]json.RawMessage
				json.Unmarshal([]byte(v.Source.LayerStackJCS), &stack)
				var layers []map[string]json.RawMessage
				json.Unmarshal(stack["layers"], &layers)
				if kind == "foreign_operation" {
					layers[0]["operation"] = json.RawMessage(`"defects.repair"`)
				}
				if kind == "disabled" {
					layers[0]["enabled"] = json.RawMessage(`false`)
				}
				if kind == "transparent" {
					layers[0]["opacity"] = json.RawMessage(`0`)
				}
				stack["layers"], _ = canonical.Marshal(layers)
				raw, err := canonical.Marshal(stack)
				if err != nil {
					t.Fatal(err)
				}
				v.Source.LayerStackJCS = string(raw)
			}
			if _, err := CompileAuthoredCurrentDetailLayers(context.Background(), v.Source, v.Params, v.Bindings); err == nil {
				t.Fatal("unbound or inactive occurrence accepted")
			}
		})
	}
}
func TestAuthoredCurrentDetailLayersPreservesCancellationBeforeInvalidInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileAuthoredCurrentDetailLayers(ctx, AuthoredInstructionSource{}, v2input.Ref{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}

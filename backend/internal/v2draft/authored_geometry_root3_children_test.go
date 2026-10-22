package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func root3Vector(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, e := os.ReadFile("testdata/" + name + "-python.json")
	if e != nil {
		t.Fatal(e)
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		t.Fatal("fixture")
	}
	return value
}
func root3Parse(t *testing.T, v map[string]any, contract string) ([]LayerRef, error) {
	t.Helper()
	raw, e := canonical.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return DocumentChildren(raw, contract, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)})
}
func TestRoot3TypedPythonVectorAndHistoricalDiscriminator(t *testing.T) {
	for _, contract := range []string{"AuthoredGeometryPhaseResultV3", "AuthoredGeometryPhaseResult"} {
		children, e := root3Parse(t, root3Vector(t, "root3"), contract)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, c := range children {
			if c.Contract == "AuthoredGeometryMaskMappingV2" {
				found = true
			}
		}
		if !found {
			t.Fatal("mapping edge lost")
		}
	}
	v := root3Vector(t, "root3")
	delete(v, "mask_mapping")
	v["schema_version"] = float64(1)
	if _, e := root3Parse(t, v, "AuthoredGeometryPhaseResult"); e != nil {
		t.Fatal("historical root1", e)
	}
	if _, e := root3Parse(t, v, "AuthoredGeometryPhaseResultV3"); e == nil {
		t.Fatal("root1 relabel")
	}
}
func TestRoot3ReportAndMappingHostiles(t *testing.T) {
	for _, kind := range []string{"report", "baseline", "mapping", "version", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			v := root3Vector(t, "root3")
			blend := v["blend"].(map[string]any)
			native := v["native"].(map[string]any)
			switch kind {
			case "report":
				blend["registration_report"] = v["registered_original"]
			case "baseline":
				blend["baseline"] = native["image"]
			case "mapping":
				v["mask_mapping"] = v["blended_image"]
			case "version":
				v["schema_version"] = float64(2)
			case "unknown":
				v["untrusted"] = true
			}
			if _, e := root3Parse(t, v, "AuthoredGeometryPhaseResultV3"); e == nil {
				t.Fatal("accepted hostile")
			}
		})
	}
	v := root3Vector(t, "root3")
	v["native"].(map[string]any)["operation"] = "image.upscale"
	b := v["blend"].(map[string]any)
	b["operation"] = "image.upscale"
	b["baseline_kind"] = "working_linear_resize"
	children, e := root3Parse(t, v, "AuthoredGeometryPhaseResultV3")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, c := range children {
		if c.Contract == "UpscaleReport" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("reports %d", count)
	}
}
func TestMapping2TypedWitnessesAreNotArtifactEdges(t *testing.T) {
	v := root3Vector(t, "mapping2")
	children, e := root3Parse(t, v, "AuthoredGeometryMaskMappingV2")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range children {
		if c.Contract == "NpyArray" {
			t.Fatal("witness digest became artifact")
		}
	}
	occ := v["local_occurrences"].([]any)
	v["local_occurrences"] = append(occ, occ[0])
	if _, e := root3Parse(t, v, "AuthoredGeometryMaskMappingV2"); e != nil {
		t.Fatal("lawful repeated leaf", e)
	}
}
func TestMapping2ClosedBoundsAndConflicts(t *testing.T) {
	for _, kind := range []string{"digest", "size", "duplicate", "path", "prefix", "union", "null", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			v := root3Vector(t, "mapping2")
			w := v["local_masks"].([]any)[0].(map[string]any)
			switch kind {
			case "digest":
				w["pixels_sha256"] = "bad"
			case "size":
				w["bytes"] = float64(0)
			case "duplicate":
				v["local_masks"] = []any{w, w}
			case "path":
				v["local_occurrences"].([]any)[0].(map[string]any)["path"] = []any{"foreign"}
			case "prefix":
				v["ordinal"] = float64(2)
			case "union":
				v["local_protection"] = v["global_protection"].(map[string]any)["current"]
			case "null":
				v["local_masks"] = nil
			case "unknown":
				w["current"] = v["target_image"]
			}
			if _, e := root3Parse(t, v, "AuthoredGeometryMaskMappingV2"); e == nil {
				t.Fatal("accepted hostile")
			}
		})
	}
}

func TestGeometryParamsActualPythonVectorTypedAndGeneric(t *testing.T) {
	for _, contract := range []string{"AuthoredGeometryTaskParams", "Params"} {
		v := root3Vector(t, "geometry-params")
		children, e := root3Parse(t, v, contract)
		if e != nil {
			t.Fatal(e)
		}
		types := map[string]bool{}
		for _, c := range children {
			types[c.Contract] = true
		}
		for _, kind := range []string{"FrozenRenderRecipe", "AuthoredBaseReceipt", "WorkingImage", "Params", "ImageResult", "ImageReport"} {
			if !types[kind] {
				t.Fatalf("missing %s", kind)
			}
		}
	}
}
func TestGeometryParamsCannotFallBackOrRebindScope(t *testing.T) {
	for _, kind := range []string{"partial", "instruction", "prefix", "native-run", "root", "profile", "alias", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			v := root3Vector(t, "geometry-params")
			native := v["native"].(map[string]any)
			task := native["task"].(map[string]any)
			switch kind {
			case "partial":
				delete(v, "native")
			case "instruction":
				v["instruction_id"] = uuid.New().String()
			case "prefix":
				v["ordinal"] = float64(2)
			case "native-run":
				task["producer_run_id"] = v["parent_run_id"]
			case "root":
				native["root"] = v["base_receipt"]
			case "profile":
				task["execution_profile_jcs"] = "{}"
			case "alias":
				v["parallel_source"] = v["recipe"]
			case "unknown":
				v["future"] = true
			}
			if _, e := root3Parse(t, v, "Params"); e == nil {
				t.Fatal("generic fallback accepted hostile")
			}
		})
	}
}

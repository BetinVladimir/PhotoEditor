package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func geometryContinuationFixture(t *testing.T) (AuthoredInstructionSource, authoredGeometryProducerObservation, map[v2input.Ref][]byte, map[v2input.Ref]string) {
	t.Helper()
	read := func(name string) map[string]any {
		raw, e := os.ReadFile("testdata/geometry-continuation-" + name + "-python.json")
		if e != nil {
			t.Fatal(e)
		}
		var out map[string]any
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal("fixture")
		}
		return out
	}
	root := read("root3")
	mapping := read("mapping2")
	wire := func(v any) []byte {
		b, e := canonical.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	ref := func(v any) v2input.Ref {
		var r v2input.Ref
		if json.Unmarshal(wire(v), &r) != nil {
			t.Fatal("ref")
		}
		return r
	}
	id := func(v any) uuid.UUID { return uuid.MustParse(v.(string)) }
	newref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source := AuthoredInstructionSource{Tenant: id(root["tenant_id"]), Parent: id(root["parent_run_id"]), OriginalRecipe: ref(root["recipe"]), ProgramSHA: root["program_sha256"].(string), PredecessorSHA: mapping["predecessor_sha256"].(string)}
	if json.Unmarshal(wire(root["input_source"]), &source.Source) != nil {
		t.Fatal("binding")
	}
	source.Instruction.Kind = "geometry"
	source.Instruction.Identity = id(root["instruction_id"])
	source.Instruction.Ordinal = int(mapping["ordinal"].(float64))
	pixels, report, changed, maskPixels := newref(), newref(), newref(), newref()
	imageRef, resultRef, mappingRef := ref(root["blended_image"]), ref(root["blended_result"]), ref(root["mask_mapping"])
	blend := root["blend"].(map[string]any)
	image := map[string]any{"pixels": pixels, "alpha": nil, "canvas": blend["target"], "color": blend["color"], "dtype": "float32", "transform_chain": blend["reference_chain"]}
	mask := map[string]any{"artifact": maskPixels, "canvas": blend["target"], "transform_chain": blend["reference_chain"], "selector": "changed", "confidence": 1, "protected": false, "object_id": nil, "model_lock_id": nil}
	imageResult := map[string]any{"schema_version": 1, "working_image": imageRef, "report": report, "changed_mask": changed, "generated_mask": nil}
	imageReport := map[string]any{"schema_version": 1, "status": "review", "operation": "render.authored_geometry", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []any{source.Source}, "registrations": []any{}}
	rootRaw := wire(root)
	rootRef := newref()
	digest := sha256.Sum256(rootRaw)
	rootRef.SHA256 = hex.EncodeToString(digest[:])
	profile := "{\"backend\":\"cpu\"}"
	ph := sha256.Sum256([]byte(profile))
	task := AuthoredPhaseTask{RunID: id(mapping["producer_run_id"]), TaskID: id(mapping["producer_task_id"]), ResultID: uuid.New(), Operation: "render.authored_geometry", Version: "1", Attempt: int(mapping["attempt"].(float64)), Fence: int64(mapping["fence"].(float64)), Params: newref(), InputDigest: strings.Repeat("a", 64), ProfileJCS: profile, ProfileSHA: hex.EncodeToString(ph[:]), Outputs: []v2input.Ref{rootRef, mappingRef, resultRef, imageRef, report, pixels, changed, maskPixels}}
	n := root["native"].(map[string]any)
	native := AuthoredPhaseTask{RunID: id(n["run_id"]), TaskID: id(n["task_id"]), ResultID: id(n["result_id"]), Operation: n["operation"].(string), Version: n["operation_version"].(string), Attempt: int(n["attempt"].(float64)), Fence: int64(n["fence"].(float64)), Params: ref(n["params"]), InputDigest: strings.Repeat("b", 64), ProfileJCS: profile, ProfileSHA: hex.EncodeToString(ph[:]), Outputs: []v2input.Ref{ref(n["root"])}}
	o := authoredGeometryProducerObservation{Context: source, RootJCS: string(rootRaw), MappingJCS: string(wire(mapping)), Producer: task, Native: native}
	o.NativeObjects = []authoredGeometryProducerObject{{Ref: ref(n["root"])}, {Ref: ref(n["image"])}, {Ref: ref(n["report"])}, {Ref: ref(n["params"])}}
	o.Scope.Root = rootRef
	docs := map[v2input.Ref][]byte{rootRef: rootRaw, mappingRef: wire(mapping), resultRef: wire(imageResult), imageRef: wire(image), report: wire(imageReport), changed: wire(mask)}
	nativeImage, nativeReport, nativeRoot := ref(n["image"]), ref(n["report"]), ref(n["root"])
	docs[nativeImage] = wire(image)
	docs[nativeReport] = wire(imageReport)
	docs[ref(n["params"])] = wire(map[string]any{"image": source.Source.Source})
	docs[nativeRoot] = wire(map[string]any{"schema_version": 1, "working_image": nativeImage, "report": nativeReport, "changed_mask": changed, "generated_mask": nil})
	roles := map[v2input.Ref]string{rootRef: "authored_phase", mappingRef: "authored_mask_mapping", resultRef: "document", imageRef: "working_image", report: "report", pixels: "pixels", changed: "mask", maskPixels: "mask_pixels"}
	return source, o, docs, roles
}
func TestAuthoredGeometryCanonicalRoot3PhaseReceipt(t *testing.T) {
	s, o, d, r := geometryContinuationFixture(t)
	got, e := validateAuthoredGeometryProducerDocuments(s, o, d, r)
	if e != nil {
		t.Fatal(e)
	}
	if got.result != o.Scope.Root || got.output.Source == got.native.Image || got.native.Task.TaskID == got.task.TaskID {
		t.Fatal("root3/native/blend conflated")
	}
}
func TestAuthoredGeometryCanonicalSubstitutions(t *testing.T) {
	for _, name := range []string{"parent", "instruction", "program", "predecessor", "rootrole", "mappingbytes", "nativefence", "nativetask", "nativerootmissing", "nativerootimage", "nativerootreport", "nativelocator", "mappingnative", "mappingsource", "mappingproducer", "outputrole"} {
		t.Run(name, func(t *testing.T) {
			s, o, d, r := geometryContinuationFixture(t)
			if _, e := validateAuthoredGeometryProducerDocuments(s, o, d, r); e != nil {
				t.Fatal("positive", e)
			}
			switch name {
			case "parent":
				s.Parent = uuid.New()
			case "instruction":
				s.Instruction.Identity = uuid.New()
			case "program":
				s.ProgramSHA = strings.Repeat("0", 64)
			case "predecessor":
				s.PredecessorSHA = strings.Repeat("0", 64)
			case "rootrole":
				r[o.Scope.Root] = "document"
			case "mappingbytes":
				o.MappingJCS = "{}"
			case "nativefence":
				o.Native.Fence++
			case "nativetask":
				o.Native.TaskID = uuid.New()
			case "nativerootmissing":
				o.Native.Outputs = nil
			case "nativerootimage", "nativerootreport":
				var fields map[string]any
				if json.Unmarshal(d[o.NativeObjects[0].Ref], &fields) != nil {
					t.Fatal("native root")
				}
				key := "working_image"
				if name == "nativerootreport" {
					key = "report"
				}
				fields[key] = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
				raw, err := canonical.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				d[o.NativeObjects[0].Ref] = raw
			case "nativelocator":
				o.NativeObjects[1].Ref = o.NativeObjects[2].Ref
			case "mappingnative", "mappingsource", "mappingproducer":
				for ref, role := range r {
					if role == "authored_mask_mapping" {
						var fields map[string]any
						if json.Unmarshal(d[ref], &fields) != nil {
							t.Fatal("mapping")
						}
						if name == "mappingnative" {
							fields["native"].(map[string]any)["task_id"] = uuid.New().String()
						}
						if name == "mappingsource" {
							fields["source"].(map[string]any)["asset_version_id"] = uuid.New().String()
						}
						if name == "mappingproducer" {
							fields["producer_task_id"] = uuid.New().String()
						}
						raw, err := canonical.Marshal(fields)
						if err != nil {
							t.Fatal(err)
						}
						d[ref] = raw
						o.MappingJCS = string(raw)
					}
				}
			case "outputrole":
				for ref, role := range r {
					if role == "working_image" {
						r[ref] = "document"
					}
				}
			}
			if _, e := validateAuthoredGeometryProducerDocuments(s, o, d, r); e == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
func TestAuthoredGeometryCanonicalCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (AuthoredGeometryProducerReader{}).Read(ctx, AuthoredInstructionSource{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestAuthoredGeometryNativeUpscaleReportBindsActualInput(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "lawful_input_report", true: "foreign_input_report"}[foreign], func(t *testing.T) {
			source, o, docs, roles := geometryContinuationFixture(t)
			var root, mapping map[string]any
			if json.Unmarshal(docs[o.Scope.Root], &root) != nil {
				t.Fatal("root")
			}
			var mappingRef v2input.Ref
			for ref, role := range roles {
				if role == "authored_mask_mapping" {
					mappingRef = ref
				}
			}
			if json.Unmarshal(docs[mappingRef], &mapping) != nil {
				t.Fatal("mapping")
			}
			native := root["native"].(map[string]any)
			native["operation"] = "image.upscale"
			blend := root["blend"].(map[string]any)
			blend["operation"] = "image.upscale"
			blend["baseline_kind"] = "working_linear_resize"
			native["root"] = native["image"]
			mapping["native"] = native
			o.Native.Operation = "image.upscale"
			o.Native.Outputs = []v2input.Ref{o.NativeObjects[1].Ref, o.NativeObjects[2].Ref}
			o.NativeObjects[0].Ref = o.NativeObjects[1].Ref
			wire := func(v any) []byte {
				raw, e := canonical.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				return raw
			}
			docs[o.Scope.Root] = wire(root)
			o.RootJCS = string(docs[o.Scope.Root])
			docs[mappingRef] = wire(mapping)
			o.MappingJCS = string(docs[mappingRef])
			reportImage := source.Source.Source
			makeReport := func(image v2input.Ref) []byte {
				return wire(map[string]any{"image": image, "model_id": "realesrgan_x2", "model_lock_id": strings.Repeat("a", 64), "backend": "cpu", "scale": 2, "color_adapter_id": "work_srgb_delta_v1", "tile": 256, "overlap": 32, "input_pixel_count": 4, "output_pixel_count": 16, "disabled_delta_fraction": 0, "source_clip_fraction": 0, "gamut_excess_fraction": 0, "alpha_resampled": false, "warnings": []string{}})
			}
			docs[o.NativeObjects[2].Ref] = makeReport(reportImage)
			if _, e := validateAuthoredGeometryProducerDocuments(source, o, docs, roles); e != nil {
				t.Fatal("positive baseline", e)
			}
			if foreign {
				reportImage.ArtifactID = uuid.New()
				docs[o.NativeObjects[2].Ref] = makeReport(reportImage)
				if _, e := validateAuthoredGeometryProducerDocuments(source, o, docs, roles); e == nil {
					t.Fatal("foreign report input accepted")
				}
			}
		})
	}
}

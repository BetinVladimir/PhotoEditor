package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Real Python-model root/mapping vectors, then genuine existing Go frozen
// recipe/LayerStack builders. This is content metadata, not a replay attestation.
func finalRoot3MappingFixture(t *testing.T) (AuthoredFinalDocuments, AuthoredPhaseReceipt, []byte, v2input.SessionVersionBinding) {
	return finalRoot3MappingFixtureOptions(t, false)
}
func finalRoot3MappingFixtureOptions(t *testing.T, complex bool) (AuthoredFinalDocuments, AuthoredPhaseReceipt, []byte, v2input.SessionVersionBinding) {
	t.Helper()
	load := func(name string) map[string]any {
		raw, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			t.Fatal("vector")
		}
		return m
	}
	root := load("root3-python.json")
	mapping := load("mapping2-python.json")
	bodies := map[v2input.Ref][]byte{}
	doc := func(id uuid.UUID, value any) v2input.Ref {
		raw, e := canonical.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(raw)
		ref := v2input.Ref{ArtifactID: id, SHA256: hex.EncodeToString(sum[:])}
		bodies[ref] = raw
		return ref
	}
	ref := func(value any) v2input.Ref {
		raw, _ := json.Marshal(value)
		var r v2input.Ref
		if json.Unmarshal(raw, &r) != nil {
			t.Fatal("ref")
		}
		return r
	}
	binding := func(value any) v2input.SessionVersionBinding {
		raw, _ := json.Marshal(value)
		var b v2input.SessionVersionBinding
		if json.Unmarshal(raw, &b) != nil {
			t.Fatal("binding")
		}
		return b
	}
	canvas := mapping["target_canvas"]
	chain := mapping["target_chain"]
	blend := root["blend"].(map[string]any)
	pin := ref(root["recipe"]).SHA256
	image := map[string]any{"pixels": v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, "alpha": nil, "dtype": "float32", "canvas": canvas, "transform_chain": chain, "color": blend["color"]}
	original := binding(mapping["source"])
	original.Source = doc(original.Source.ArtifactID, image)
	mask := map[string]any{"artifact": v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, "canvas": canvas, "transform_chain": chain, "object_id": nil, "selector": "all", "confidence": 1, "model_lock_id": nil, "protected": false}
	layerMask := doc(ref(mapping["layer_mask"]).ArtifactID, mask)
	mapping["layer_mask"] = layerMask
	blend["target_mask"] = layerMask
	mapping["global_protection"] = nil
	mapping["local_protection"] = nil
	blend["target_protection"] = nil
	target := doc(ref(root["blended_image"]).ArtifactID, image)
	root["blended_image"] = target
	registered := doc(ref(root["registered_original"]).ArtifactID, image)
	baseline := doc(ref(blend["baseline"]).ArtifactID, image)
	root["registered_original"] = registered
	blend["baseline"] = baseline
	mapping["target_image"] = target
	root["original_source"] = original
	root["input_source"] = original
	mapping["source"] = original
	blend["sources"] = []v2input.SessionVersionBinding{original}
	leaf := mapping["local_masks"].([]any)[0].(map[string]any)["original"]
	expression := map[string]any{"op": "ref", "mask": leaf, "canvas_hash": original.CanvasHash}
	var protectedOriginal any
	extra := []v2input.Ref{}
	if complex {
		leafB := v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}
		a := map[string]any{"op": "ref", "mask": leaf, "canvas_hash": original.CanvasHash}
		b := map[string]any{"op": "ref", "mask": leafB, "canvas_hash": original.CanvasHash}
		expression = map[string]any{"op": "union", "left": a, "right": map[string]any{"op": "subtract", "left": map[string]any{"op": "invert", "operand": b}, "right": a}}
		mapping["local_occurrences"] = []any{map[string]any{"path": []string{"left"}, "original": leaf}, map[string]any{"path": []string{"right", "left", "operand"}, "original": leafB}, map[string]any{"path": []string{"right", "right"}, "original": leaf}}
		first := mapping["local_masks"].([]any)[0]
		mapping["local_masks"] = []any{first, map[string]any{"original": leafB, "pixels_sha256": pin, "bytes": 192}}
		globalMask := map[string]any{}
		for k, v := range mask {
			globalMask[k] = v
		}
		globalMask["protected"] = true
		globalMask["selector"] = "authored_global"
		currentGlobal := doc(uuid.New(), globalMask)
		globalMask["selector"] = "authored_protection"
		union := doc(uuid.New(), globalMask)
		mapping["global_protection"] = map[string]any{"original": leaf, "current": currentGlobal}
		mapping["local_protection"] = union
		blend["target_protection"] = union
		protectedOriginal = leaf
		extra = append(extra, currentGlobal, union)
	}
	expressionSHA, _ := canonical.Hash(expression)
	mapping["local_expression_sha256"] = expressionSHA
	// Keep immutable Python occurrence/leaf ordering; one leaf at the empty path.
	layer := map[string]any{"id": blend["layer_id"], "enabled": true, "opacity": blend["opacity"], "blend": blend["blend"], "operation": blend["operation"], "operation_version": "1", "params": blend["params"], "mask": expression}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": original, "layers": []any{layer}})
	stackRef := doc(uuid.New(), json.RawMessage(stack))
	authoredFields := map[string]any{"schema_version": 3, "source": original, "base_style": v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": stackRef, "protected_mask": protectedOriginal, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}}
	if protectedOriginal == nil { delete(authoredFields, "protected_mask") }
	authored, _ := canonical.Marshal(authoredFields)
	var authoredDoc struct {
		Base v2input.Ref `json:"base_style"`
	}
	json.Unmarshal(authored, &authoredDoc)
	frozen, e := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: original, BaseStyle: authoredDoc.Base, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: pin, ModelLockSHA: pin}, stack)
	if e != nil {
		t.Fatal(e)
	}
	recipe := doc(ref(root["recipe"]).ArtifactID, json.RawMessage(frozen))
	root["recipe"] = recipe
	mapping["recipe"] = recipe
	blend["recipe_sha256"] = recipe.SHA256
	mappingRef := doc(ref(root["mask_mapping"]).ArtifactID, mapping)
	root["mask_mapping"] = mappingRef
	rootRef := doc(uuid.New(), root)
	var m finalRoot3Mapping
	if decodeAuthoredRoot3Mapping(bodies[mappingRef], mappingRef, &m) != nil {
		t.Fatal("mapping vector")
	}
	var tenant, parent, instruction uuid.UUID
	tenant = uuid.MustParse(root["tenant_id"].(string))
	parent = uuid.MustParse(root["parent_run_id"].(string))
	instruction = uuid.MustParse(root["instruction_id"].(string))
	phase := AuthoredPhaseReceipt{AuthoredPhaseTask: AuthoredPhaseTask{RunID: m.Run, TaskID: m.Task, Attempt: m.Attempt, Fence: m.Fence}, Tenant: tenant, Parent: parent, Instruction: instruction, Ordinal: 1, ProgramSHA: root["program_sha256"].(string), OriginalRecipe: recipe, PredecessorSHA: m.Predecessor, InputSource: original, OutputSource: v2input.SessionVersionBinding{AssetVersionID: original.AssetVersionID, Source: target, CanvasHash: original.CanvasHash}, Result: rootRef, PhysicalClosure: []v2input.Ref{mappingRef, layerMask, registered, baseline}}
	phase.PhysicalClosure = append(phase.PhysicalClosure, extra...)
	source := AuthoredInstructionSource{Tenant: tenant, Parent: parent, OriginalRecipe: recipe, RecipeJCS: string(frozen), LayerStackJCS: string(stack), Instruction: AuthoredGeometryInstruction{Ordinal: 2, Kind: "finalize"}}
	return AuthoredFinalDocuments{Inventory: AuthoredFinalInventory{Context: source}, Bodies: bodies}, phase, bodies[rootRef], original
}
func TestFinalRoot3MappingConsumesActualTypedPythonRootAndOriginalDSL(t *testing.T) {
	d, p, raw, original := finalRoot3MappingFixture(t)
	if e := verifyAuthoredFinalRoot3Mapping(d, p, raw, original, []v2input.Ref{}); e != nil {
		t.Fatal(e)
	}
}
func TestFinalRoot3MappingRefusesMissingPhysicalAndForeignAuthority(t *testing.T) {
	for _, name := range []string{"missing-map", "root-bytes", "foreign-producer", "foreign-source", "wrong-prefix", "missing-retention", "original-stack"} {
		t.Run(name, func(t *testing.T) {
			d, p, raw, original := finalRoot3MappingFixture(t)
			prior := []v2input.Ref{}
			switch name {
			case "missing-map":
				var r struct {
					Mapping v2input.Ref `json:"mask_mapping"`
				}
				json.Unmarshal(raw, &r)
				delete(d.Bodies, r.Mapping)
			case "root-bytes":
				raw = append(raw, ' ')
			case "foreign-producer":
				p.TaskID = uuid.New()
			case "foreign-source":
				p.InputSource.Source.ArtifactID = uuid.New()
			case "wrong-prefix":
				prior = append(prior, p.Result)
			case "missing-retention":
				p.PhysicalClosure = nil
			case "original-stack":
				d.Inventory.Context.LayerStackJCS = "{}"
			}
			if verifyAuthoredFinalRoot3Mapping(d, p, raw, original, prior) == nil {
				t.Fatal("accepted", name)
			}
		})
	}
}

func TestFinalRoot3MappingComplexRepeatedExpressionAndGlobal(t *testing.T) {
	d, p, raw, original := finalRoot3MappingFixtureOptions(t, true)
	if err := verifyAuthoredFinalRoot3Mapping(d, p, raw, original, []v2input.Ref{}); err != nil {
		t.Fatal(err)
	}
}

func TestFinalRoot3MappingRehashedComplexAuthorityMutants(t *testing.T) {
	for _, name := range []string{"occurrence-order", "occurrence-path", "leaf-identity", "native-tuple", "params", "source", "canvas", "global-original", "global-current", "union-pixels"} {
		t.Run(name, func(t *testing.T) {
			d, p, raw, original := finalRoot3MappingFixtureOptions(t, true)
			if err := verifyAuthoredFinalRoot3Mapping(d, p, raw, original, []v2input.Ref{}); err != nil {
				t.Fatalf("positive before mutant: %v", err)
			}
			var root map[string]any
			if json.Unmarshal(raw, &root) != nil {
				t.Fatal("root")
			}
			ref := func(v any) v2input.Ref {
				b, _ := json.Marshal(v)
				var r v2input.Ref
				if json.Unmarshal(b, &r) != nil {
					t.Fatal("ref")
				}
				return r
			}
			oldMap := ref(root["mask_mapping"])
			var m map[string]any
			if json.Unmarshal(d.Bodies[oldMap], &m) != nil {
				t.Fatal("mapping")
			}
			replace := func(old v2input.Ref, v any) v2input.Ref {
				b, e := canonical.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				s := sha256.Sum256(b)
				r := v2input.Ref{ArtifactID: old.ArtifactID, SHA256: hex.EncodeToString(s[:])}
				d.Bodies[r] = b
				for i, x := range p.PhysicalClosure {
					if x == old {
						p.PhysicalClosure[i] = r
					}
				}
				return r
			}
			foreign := v2input.Ref{ArtifactID: uuid.New(), SHA256: original.Source.SHA256}
			switch name {
			case "occurrence-order":
				a := m["local_occurrences"].([]any)
				a[0], a[1] = a[1], a[0]
			case "occurrence-path":
				m["local_occurrences"].([]any)[2].(map[string]any)["path"] = []string{"right", "left"}
			case "leaf-identity":
				m["local_masks"].([]any)[1].(map[string]any)["original"] = foreign
			case "native-tuple":
				m["native"].(map[string]any)["task_id"] = uuid.New()
			case "params":
				m["native"].(map[string]any)["params"] = foreign
			case "source":
				m["source"].(map[string]any)["source"] = foreign
			case "canvas":
				m["target_canvas"].(map[string]any)["width"] = 999
			case "global-original":
				m["global_protection"].(map[string]any)["original"] = foreign
			case "global-current":
				m["global_protection"].(map[string]any)["current"] = m["layer_mask"]
			case "union-pixels":
				old := ref(m["local_protection"])
				var mask map[string]any
				json.Unmarshal(d.Bodies[old], &mask)
				mask["artifact"] = foreign
				next := replace(old, mask)
				m["local_protection"] = next
				root["blend"].(map[string]any)["target_protection"] = next
			}
			root["mask_mapping"] = replace(oldMap, m)
			p.Result = replace(p.Result, root)
			raw = d.Bodies[p.Result]
			if verifyAuthoredFinalRoot3Mapping(d, p, raw, original, []v2input.Ref{}) == nil {
				t.Fatal("accepted canonical rehashed mutant", name)
			}
		})
	}
}

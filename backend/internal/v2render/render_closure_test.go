package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"testing"
)

type fullRenderClosureStore struct {
	objects map[uuid.UUID]objectstore.Object
	bodies  map[string][]byte
}

type renderPostIORevocation struct {
	v2draft.ArtifactCatalog
	calls int
}

type renderConsentRefusal struct { err error }

func (s renderConsentRefusal) Lookup(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error) {
	return objectstore.Object{}, s.err
}

func (s *renderPostIORevocation) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	s.calls++
	if s.calls > 1 {
		return objectstore.Object{}, v2input.ErrExpired
	}
	return s.ArtifactCatalog.Lookup(ctx, who, tenant, node)
}

func (s fullRenderClosureStore) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	obj, ok := s.objects[node.Ref.ArtifactID]
	if !ok || obj.SHA256 != node.Ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return obj, nil
}
func (s fullRenderClosureStore) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.bodies[obj.Key])), nil
}

func TestResolveRenderInputsWithActualDocumentClosure(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{false: "base", true: "protected"}[protected], func(t *testing.T) { testResolveRenderInputsWithActualDocumentClosure(t, protected) })
	}
}

func testResolveRenderInputsWithActualDocumentClosure(t *testing.T, protected bool) {
	ctx := context.Background()
	store := fullRenderClosureStore{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	put := func(raw []byte) v2input.Ref {
		t.Helper()
		sum := sha256.Sum256(raw)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		key := ref.ArtifactID.String()
		store.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256, Bytes: int64(len(raw)), VersionID: "v1", ContentType: "application/json"}
		store.bodies[key] = raw
		return ref
	}
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	source := images[0]
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	sourceRef := put(source)
	canvasSHA, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: canvasSHA}
	store.objects[image.Pixels.ID] = objectstore.Object{Key: "pixels", SHA256: image.Pixels.SHA256, Bytes: 64, VersionID: "v1", ContentType: "application/x-npy"}
	styleRaw, err := os.ReadFile("../../../docs/implementation/contracts/examples/style-fixed.example.json")
	if err != nil {
		t.Fatal(err)
	}
	styleRaw, err = canonical.Bytes(styleRaw)
	if err != nil {
		t.Fatal(err)
	}
	style := put(styleRaw)
	var protectionRaw []byte
	var protectionRef *v2input.Ref
	if protected {
		pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
		store.objects[pixels.ArtifactID] = objectstore.Object{Key: "protection.npy", SHA256: pixels.SHA256, Bytes: 64, VersionID: "v1", ContentType: "application/x-npy"}
		protectionRaw, err = canonical.Marshal(review.MaskRef{Artifact: review.ArtifactRef{ID: pixels.ArtifactID, SHA256: pixels.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: true})
		if err != nil {
			t.Fatal(err)
		}
		ref := put(protectionRaw)
		protectionRef = &ref
	}
	authoredBody := map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []any{}, "layer_stack": nil, "protected_mask": protectionRef, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}}
	if protectionRef == nil {
		delete(authoredBody, "protected_mask")
	}
	authored, err := canonical.Marshal(authoredBody)
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("a", 64), ModelLockSHA: strings.Repeat("b", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := put(recipe)
	resolver := v2draft.ClosureResolver{Catalog: store, Objects: store}
	actor := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	entries, err := ResolveRenderInputs(ctx, actor, tenant, binding, root, recipe, source, resolver)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Ref.ArtifactID == image.Pixels.ID {
			found = entry.Contract == "NpyArray"
		}
	}
	if !found {
		t.Fatal("actual resolver binary contract missing")
	}
	workflow := uuid.New()
	request := RenderDraftRequest{Tenant: tenant, Actor: actor, Binding: binding, RecipeRef: root,
		RecipeRaw: recipe, SourceRaw: source, ProtectionRaw: protectionRaw, Backend: "cuda", RuntimeSHA: strings.Repeat("a", 64),
		TileSize: 512, AlphaMode: "require_float32"}
	choice := RenderExecutionChoice{Backend: request.Backend, RuntimeSHA: request.RuntimeSHA, TileSize: request.TileSize, AlphaMode: request.AlphaMode}
	selection := RenderSelectionItem{Binding: binding, Recipe: root}
	loader := RenderRequestLoader{Catalog: store, Objects: store}
	consentErr := domain.NewError("PROFILE_CONSENT_REVOKED", 409)
	if _, err := (RenderRequestLoader{Catalog: renderConsentRefusal{err: consentErr}, Objects: store}).Load(ctx, tenant, actor, selection, choice); err != consentErr {
		t.Fatal("consent refusal lost at loader boundary", err)
	}
	loaded, err := loader.Load(ctx, tenant, actor, selection, choice)
	if err != nil || !bytes.Equal(loaded.RecipeRaw, recipe) || !bytes.Equal(loaded.SourceRaw, source) || !bytes.Equal(loaded.ProtectionRaw, protectionRaw) {
		t.Fatal("scoped request loading changed evidence", err)
	}
	denied := &renderPostIORevocation{ArtifactCatalog: store}
	if _, err := (RenderRequestLoader{Catalog: denied, Objects: store}).Load(ctx, tenant, actor, selection, choice); !errors.Is(err, v2input.ErrExpired) || denied.calls != 2 {
		t.Fatal("post IO revocation not repeated", err, denied.calls)
	}
	denied.calls = 0
	badChoice := choice
	badChoice.RuntimeSHA = strings.Repeat("Z", 64)
	if _, err := (RenderRequestLoader{Catalog: denied, Objects: store}).Load(ctx, tenant, actor, selection, badChoice); !errors.Is(err, v2input.ErrInvalid) || denied.calls != 0 {
		t.Fatal("invalid execution accessed catalog", err, denied.calls)
	}
	store.bodies[root.ArtifactID.String()] = append(append([]byte(nil), recipe...), ' ')
	_, corruptErr := loader.Load(ctx, tenant, actor, selection, choice)
	store.bodies[root.ArtifactID.String()] = recipe
	if !errors.Is(corruptErr, v2input.ErrInvalid) {
		t.Fatal("changed pinned bytes loaded", corruptErr)
	}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	stackRef := put(stack)
	authoredBody["layer_stack"] = stackRef
	withLayer, _ := canonical.Marshal(authoredBody)
	layerRecipe, err := v2draft.BuildFrozenRenderRecipe(withLayer, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("a", 64), ModelLockSHA: strings.Repeat("b", 64)}, stack)
	if err != nil {
		t.Fatal(err)
	}
	layerLoaded, err := loader.Load(ctx, tenant, actor, RenderSelectionItem{Binding: binding, Recipe: put(layerRecipe)}, choice)
	if err != nil || !bytes.Equal(layerLoaded.LayerRaw, stack) || !bytes.Equal(layerLoaded.ProtectionRaw, protectionRaw) {
		t.Fatal("authored child documents not loaded", err)
	}
	paramsRaw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "ev": 0.2})
	paramsRef := put(paramsRaw)
	maskPixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	store.objects[maskPixels.ArtifactID] = objectstore.Object{Key: "layer-mask.npy", SHA256: maskPixels.SHA256, Bytes: 64, VersionID: "v1", ContentType: "application/x-npy"}
	maskRaw, _ := canonical.Marshal(review.MaskRef{Artifact: review.ArtifactRef{ID: maskPixels.ArtifactID, SHA256: maskPixels.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: false})
	maskRef := put(maskRaw)
	nonempty, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{map[string]any{
		"id": uuid.New(), "enabled": false, "opacity": 0, "blend": "normal", "operation": "adjust.exposure", "operation_version": "1",
		"params": paramsRef, "mask": map[string]any{"op": "ref", "mask": maskRef, "canvas_hash": binding.CanvasHash}}}})
	authoredBody["layer_stack"] = put(nonempty)
	nonemptyAuthored, _ := canonical.Marshal(authoredBody)
	nonemptyRecipe, e := v2draft.BuildFrozenRenderRecipe(nonemptyAuthored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("a", 64), ModelLockSHA: strings.Repeat("b", 64)}, nonempty)
	if e != nil {
		t.Fatal(e)
	}
	nonemptyRef := put(nonemptyRecipe)
	loadedStack, e := loader.Load(ctx, tenant, actor, RenderSelectionItem{Binding: binding, Recipe: nonemptyRef}, choice)
	if e != nil || len(loadedStack.LayerDocuments) != 2 || loadedStack.LayerDocuments[0].JCS != string(paramsRaw) || loadedStack.LayerDocuments[1].JCS != string(maskRaw) {
		t.Fatal("scoped layer documents not retained", e)
	}
	fullEvidence, e := resolveRenderClosureEvidence(ctx, loadedStack, resolver)
	layerEntries, closureDocuments := fullEvidence.Entries, fullEvidence.Documents
	loadedStack.ClosureOccurrences = fullEvidence.Occurrences
	if e != nil {
		t.Fatal(e)
	}
	loadedStack.ClosureDocuments = closureDocuments
	if len(closureDocuments) == 0 {
		t.Fatal("scoped resolver did not expose closure evidence")
	}
	for _, layer := range loadedStack.LayerDocuments {
		found := false
		for _, witness := range closureDocuments {
			if witness.Ref == layer.Ref && witness.Contract == layer.Contract {
				if witness.Parent != layer.Parent || !strings.Contains(witness.Path, ".layer_stack.layers[") {
					t.Fatal("layer occurrence lost exact parent/path", witness)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("layer child missing from scoped read evidence")
		}
	}
	layerRun, layerProject, layerSnapshot := uuid.New(), uuid.New(), uuid.New()
	layerPlan, e := CompileRenderRunPlan(layerRun, layerProject, layerSnapshot, []RenderDraftRequest{loadedStack}, [][]v2input.Entry{layerEntries})
	if e != nil {
		t.Fatal("layer witnesses did not freeze", e)
	}
	layerSaved := RecoveredRenderDraft{RunID: layerRun, WorkflowID: layerPlan.Workflow, ProjectID: layerProject, SnapshotID: layerSnapshot, Config: layerPlan.Config, ConfigSHA: layerPlan.ConfigSHA, ReservedBytes: layerPlan.OutputBytes}
	recoveredStack, e := DecodeRecoveredRenderPlan(layerSaved)
	if e != nil || len(recoveredStack) != 1 || len(recoveredStack[0].Node.LayerDocuments) != 2 {
		t.Fatal("layer witnesses did not recover", e)
	}
	if len(recoveredStack[0].Node.ClosureDocuments) != len(closureDocuments) {
		t.Fatal("closure read evidence lost during recovery")
	}
	for _, removed := range []uuid.UUID{nonemptyRef.ArtifactID, binding.Source.ArtifactID, loadedStack.LayerDocuments[0].Ref.ArtifactID, loadedStack.LayerDocuments[1].Ref.ArtifactID} {
		var changed map[string]any
		if json.Unmarshal(layerSaved.Config, &changed) != nil {
			t.Fatal("saved closure evidence")
		}
		node := changed["v26_render"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
		kept := []any{}
		for _, entry := range node["closure_documents"].([]any) {
			if entry.(map[string]any)["Ref"].(map[string]any)["artifact_id"] != removed.String() {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 || len(kept) == len(node["closure_documents"].([]any)) {
			t.Fatal("coverage fixture must remove a known JSON ref while retaining evidence")
		}
		node["closure_documents"] = kept
		incomplete := layerSaved
		incomplete.Config, e = canonical.Marshal(changed)
		if e != nil {
			t.Fatal(e)
		}
		incomplete.ConfigSHA, e = canonical.Hash(json.RawMessage(incomplete.Config))
		if e != nil {
			t.Fatal(e)
		}
		if _, e := DecodeRecoveredRenderPlan(incomplete); !errors.Is(e, v2input.ErrInvalid) {
			t.Fatal("rehashed incomplete JSON evidence recovered", removed, e)
		}
	}
	pinnedParams := loadedStack.LayerDocuments[0].Ref.ArtifactID
	originalObject := store.objects[pinnedParams]
	driftedObject := originalObject
	driftedObject.VersionID = "new-version-with-same-sha"
	store.objects[pinnedParams] = driftedObject
	driftProbe := &renderCoordinatorProbe{}
	driftCoordinator := RenderDraftCoordinator{Inputs: driftProbe, Transport: driftProbe, Resolver: resolver}
	driftErr := driftCoordinator.RecoverInputs(ctx, tenant, actor, layerSaved)
	store.objects[pinnedParams] = originalObject
	if !errors.Is(driftErr, v2input.ErrExpired) || len(driftProbe.events) != 0 {
		t.Fatal("same-SHA version drift published during recovery", driftErr, driftProbe.events)
	}
	var binaryID uuid.UUID
	for _, occurrence := range fullEvidence.Occurrences {
		if occurrence.Node.Contract == "WorkingPixels" {
			binaryID = occurrence.Node.Ref.ArtifactID
			break
		}
	}
	if binaryID == uuid.Nil {
		t.Fatal("missing binary occurrence fixture")
	}
	binaryObject := store.objects[binaryID]
	changedBinary := binaryObject
	changedBinary.VersionID = "changed-binary-version"
	store.objects[binaryID] = changedBinary
	driftProbe.events = nil
	binaryErr := driftCoordinator.RecoverInputs(ctx, tenant, actor, layerSaved)
	store.objects[binaryID] = binaryObject
	if !errors.Is(binaryErr, v2input.ErrExpired) || len(driftProbe.events) != 0 {
		t.Fatal("binary version drift published during recovery", binaryErr, driftProbe.events)
	}
	var corrupt map[string]any
	if json.Unmarshal(layerSaved.Config, &corrupt) != nil {
		t.Fatal("saved evidence")
	}
	corruptNode := corrupt["v26_render"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	corruptNode["closure_documents"].([]any)[0].(map[string]any)["JCS"] = "{}"
	corruptSaved := layerSaved
	corruptSaved.Config, e = canonical.Marshal(corrupt)
	if e != nil {
		t.Fatal(e)
	}
	corruptSaved.ConfigSHA, e = canonical.Hash(json.RawMessage(corruptSaved.Config))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeRecoveredRenderPlan(corruptSaved); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("rehashed contradictory closure bytes recovered", e)
	}
	for _, document := range loadedStack.LayerDocuments {
		incomplete := make([]v2input.Entry, 0, len(layerEntries)-1)
		for _, entry := range layerEntries {
			if entry.Ref != document.Ref {
				incomplete = append(incomplete, entry)
			}
		}
		if len(incomplete) != len(layerEntries)-1 {
			t.Fatal("fixture witness absent from inventory")
		}
		if _, err := ResolveRenderInputs(ctx, actor, tenant, binding, nonemptyRef, nonemptyRecipe, source, renderResolvedInventory{incomplete}); err != nil {
			t.Fatal("fixture must pass root inventory validation to exercise the witness gate", err)
		}
		var changed map[string]any
		if json.Unmarshal(layerSaved.Config, &changed) != nil {
			t.Fatal("saved fixture")
		}
		node := changed["v26_render"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
		digest, err := canonical.Hash(struct {
			Task      uuid.UUID       `json:"task"`
			Operation string          `json:"operation"`
			Version   string          `json:"version"`
			Entries   []v2input.Entry `json:"entries"`
		}{layerPlan.Tasks[0].ID, "render.compose", "1", incomplete})
		if err != nil {
			t.Fatal(err)
		}
		node["input_digest"] = digest
		contradictory := layerSaved
		contradictory.Config, err = canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		contradictory.ConfigSHA, err = canonical.Hash(json.RawMessage(contradictory.Config))
		if err != nil {
			t.Fatal(err)
		}
		probe := &renderCoordinatorProbe{}
		coordinator := RenderDraftCoordinator{Inputs: probe, Transport: probe, Resolver: renderResolvedInventory{incomplete}}
		if err := coordinator.RecoverInputs(ctx, tenant, actor, contradictory); !errors.Is(err, v2input.ErrInvalid) || len(probe.events) != 0 {
			t.Fatal("rehashed incomplete recovery published", err, probe.events)
		}
	}
	for _, mutation := range []string{"missing", "extra", "parent", "content", "order", "closure_missing", "closure_empty", "closure_parent", "closure_path", "closure_cycle", "closure_opaque_child", "occurrences_missing", "occurrences_empty", "occurrences_duplicate_path", "binary_occurrence_missing", "binary_parent", "binary_path", "binary_root", "binary_binding", "binary_learning", "binary_frame", "binary_model", "binary_version", "binary_key", "binary_size", "binary_media"} {
		var changed map[string]any
		if json.Unmarshal(layerSaved.Config, &changed) != nil {
			t.Fatal("cannot decode saved witnesses")
		}
		node := changed["v26_render"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
		documents := node["layer_documents"].([]any)
		switch mutation {
		case "binary_parent", "binary_path", "binary_root", "binary_binding", "binary_learning", "binary_frame", "binary_model", "binary_version", "binary_key", "binary_size", "binary_media":
			found := false
			for _, raw := range node["closure_occurrences"].([]any) {
				occurrence := raw.(map[string]any)["Node"].(map[string]any)
				if occurrence["Contract"] != "WorkingPixels" {
					continue
				}
				switch mutation {
				case "binary_version":
					raw.(map[string]any)["Object"].(map[string]any)["VersionID"] = "another-immutable-version"
				case "binary_key":
					raw.(map[string]any)["Object"].(map[string]any)["Key"] = "another-immutable-key"
				case "binary_size":
					obj := raw.(map[string]any)["Object"].(map[string]any)
					obj["Bytes"] = obj["Bytes"].(float64) + 1
				case "binary_media":
					raw.(map[string]any)["Object"].(map[string]any)["ContentType"] = "application/octet-stream"
				case "binary_binding":
					occurrence["ExpectedBinding"] = v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
				case "binary_learning":
					occurrence["ExpectedLearning"] = v2draft.FrozenRenderLearning{VersionID: uuid.New()}
				case "binary_frame":
					occurrence["RenderFrame"] = v2draft.RenderFrameAuthority{SelectedVersion: uuid.New()}
				case "binary_model":
					occurrence["ExpectedModelLockSHA"] = strings.Repeat("b", 64)
				case "binary_parent":
					occurrence["ParentRef"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
				case "binary_path":
					occurrence["Path"] = occurrence["Path"].(string) + ".unexpected"
				case "binary_root":
					occurrence["ParentRef"] = v2input.Ref{}
				}
				found = true
				break
			}
			if !found {
				t.Fatal("missing binary occurrence fixture")
			}
		case "binary_occurrence_missing":
			occurrences := node["closure_occurrences"].([]any)
			removed := false
			for i, raw := range occurrences {
				n := raw.(map[string]any)["Node"].(map[string]any)
				if n["Contract"] == "WorkingPixels" && strings.Contains(n["Path"].(string), ".recipe.source.source.pixels") {
					node["closure_occurrences"] = append(occurrences[:i], occurrences[i+1:]...)
					removed = true
					break
				}
			}
			if !removed {
				t.Fatal("missing repeated pixel fixture")
			}
		case "occurrences_duplicate_path":
			occurrences := node["closure_occurrences"].([]any)
			node["closure_occurrences"] = append(occurrences, occurrences[0])
		case "occurrences_missing":
			delete(node, "closure_occurrences")
		case "occurrences_empty":
			node["closure_occurrences"] = []any{}
		case "closure_opaque_child":
			var parent v2draft.DocumentWitness
			for _, witness := range closureDocuments {
				if witness.Contract == "StyleV2" {
					parent = witness
					break
				}
			}
			if parent.Ref.ArtifactID == uuid.Nil {
				t.Fatal("missing opaque parent fixture")
			}
			sum := sha256.Sum256([]byte("{}"))
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			node["closure_documents"] = append(node["closure_documents"].([]any), v2draft.DocumentWitness{
				Path: parent.Path + ".unexpected", Contract: "Artifact", Ref: ref, Parent: parent.Ref, JCS: "{}",
				Object: objectstore.Object{Key: "opaque-child", VersionID: "v1", SHA256: ref.SHA256, ContentType: "application/json", Bytes: 2}})
		case "closure_cycle":
			sum := sha256.Sum256([]byte("{}"))
			refA := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			refB := v2input.Ref{ArtifactID: uuid.New(), SHA256: refA.SHA256}
			object := objectstore.Object{Key: "cycle", VersionID: "v1", SHA256: refA.SHA256, ContentType: "application/json", Bytes: 2}
			node["closure_documents"] = append(node["closure_documents"].([]any),
				v2draft.DocumentWitness{Path: "unreachable-a", Contract: "Artifact", Ref: refA, Parent: refB, Object: object, JCS: "{}"},
				v2draft.DocumentWitness{Path: "unreachable-b", Contract: "Artifact", Ref: refB, Parent: refA, Object: object, JCS: "{}"})
		case "closure_parent":
			node["closure_documents"].([]any)[0].(map[string]any)["Parent"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
		case "closure_path":
			for _, entry := range node["closure_documents"].([]any) {
				if entry.(map[string]any)["Contract"] == "Params" {
					entry.(map[string]any)["Path"] = "unrelated.path"
					break
				}
			}
		case "closure_missing":
			delete(node, "closure_documents")
		case "closure_empty":
			node["closure_documents"] = []any{}
		case "missing":
			node["layer_documents"] = documents[:1]
		case "extra":
			node["layer_documents"] = append(documents, documents[0])
		case "parent":
			documents[0].(map[string]any)["parent"].(map[string]any)["artifact_id"] = uuid.New().String()
		case "content":
			documents[0].(map[string]any)["jcs"] = `{"ev":1,"schema_version":1}`
		case "order":
			documents[0], documents[1] = documents[1], documents[0]
		}
		altered := layerSaved
		altered.Config, e = canonical.Marshal(changed)
		if e != nil {
			t.Fatal(e)
		}
		altered.ConfigSHA, e = canonical.Hash(json.RawMessage(altered.Config))
		if e != nil {
			t.Fatal(e)
		}
		if _, e := DecodeRecoveredRenderPlan(altered); !errors.Is(e, v2input.ErrInvalid) {
			t.Fatal("rehashed contradictory layer witness recovered", mutation, e)
		}
	}
	withoutClosure := loadedStack
	withoutClosure.ClosureDocuments = nil
	if _, _, e := CompileRenderDraft(layerPlan.Workflow, 0, withoutClosure, layerEntries); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("direct draft accepted absent closure evidence", e)
	}
	if _, _, e := resolveRenderClosureDocuments(ctx, loadedStack, renderResolvedInventory{layerEntries}); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("nonempty stack accepted a resolver without byte witnesses", e)
	}
	for _, batch := range []bool{false, true} {
		caller := loadedStack
		caller.LayerDocuments = append([]RenderLayerDocument(nil), loadedStack.LayerDocuments...)
		before := caller.LayerDocuments[0]
		mutating := &renderMutatingCallerResolver{CompositionInputResolver: resolver, mutate: func() { caller.LayerDocuments[0].JCS = "{}"; caller.LayerDocuments[0].Path = "changed" }}
		probe := &renderCoordinatorProbe{}
		coordinator := RenderDraftCoordinator{Store: probe, Inputs: probe, Transport: probe, Resolver: mutating}
		var config json.RawMessage
		if batch {
			store := &renderBatchRecoveryProbe{failPart: -1}
			plans, err := (RenderBatchCoordinator{Store: store, Inputs: coordinator}).Prepare(ctx, uuid.New(), layerProject, layerSnapshot, strings.Repeat("a", 64), []RenderDraftRequest{caller})
			if err != nil || len(plans) != 1 {
				t.Fatal("batch caller mutation reached frozen witnesses", err)
			}
			config = plans[0].Plan.Config
		} else {
			plan, err := coordinator.Prepare(ctx, uuid.New(), layerProject, layerSnapshot, strings.Repeat("a", 64), []RenderDraftRequest{caller})
			if err != nil {
				t.Fatal("caller mutation reached frozen witnesses", err)
			}
			config = plan.Config
		}
		var frozen struct {
			Render struct {
				Nodes []RenderRunNode `json:"nodes"`
			} `json:"v26_render"`
		}
		if json.Unmarshal(config, &frozen) != nil || len(frozen.Render.Nodes) != 1 || frozen.Render.Nodes[0].LayerDocuments[0] != before || caller.LayerDocuments[0] == before || len(frozen.Render.Nodes[0].ClosureDocuments) == 0 || len(frozen.Render.Nodes[0].ClosureOccurrences) == 0 {
			t.Fatal("witness ownership not detached", batch)
		}
	}
	cancelledCtx, cancelEvidence := context.WithCancel(ctx)
	cancelling := &renderCountingEvidenceResolver{CompositionInputResolver: resolver, afterEvidence: cancelEvidence}
	cancelledProbe := &renderCoordinatorProbe{}
	cancelledCoordinator := RenderDraftCoordinator{Store: cancelledProbe, Inputs: cancelledProbe, Transport: cancelledProbe, Resolver: cancelling}
	cancelledPlan, cancelledErr := cancelledCoordinator.Prepare(cancelledCtx, uuid.New(), layerProject, layerSnapshot, strings.Repeat("a", 64), []RenderDraftRequest{loadedStack, loadedStack})
	cancelEvidence()
	if !errors.Is(cancelledErr, context.Canceled) || cancelling.calls != 1 || len(cancelledProbe.events) != 0 || len(cancelledPlan.Config) != 0 || len(cancelledPlan.Tasks) != 0 {
		t.Fatal("evidence cancellation continued compilation", cancelledErr, cancelling.calls, cancelledProbe.events)
	}
	cancelledBatchCtx, cancelBatchEvidence := context.WithCancel(ctx)
	cancellingBatch := &renderCountingEvidenceResolver{CompositionInputResolver: resolver, afterEvidence: cancelBatchEvidence}
	cancelledBatchStore := &renderBatchRecoveryProbe{failPart: -1}
	cancelledBatchProbe := &renderCoordinatorProbe{}
	cancelledBatchCoordinator := RenderBatchCoordinator{Store: cancelledBatchStore, Inputs: RenderDraftCoordinator{Inputs: cancelledBatchProbe, Transport: cancelledBatchProbe, Resolver: cancellingBatch}}
	cancelledParts, cancelledBatchErr := cancelledBatchCoordinator.Prepare(cancelledBatchCtx, uuid.New(), layerProject, layerSnapshot, strings.Repeat("a", 64), []RenderDraftRequest{loadedStack})
	cancelBatchEvidence()
	if !errors.Is(cancelledBatchErr, context.Canceled) || cancellingBatch.calls != 1 || len(cancelledParts) != 0 || cancelledBatchStore.begins != 0 || cancelledBatchStore.loads != 0 || len(cancelledBatchProbe.events) != 0 {
		t.Fatal("batch evidence cancellation continued", cancelledBatchErr, cancellingBatch.calls, cancelledBatchProbe.events)
	}
	evidenceSize := 0
	for _, document := range fullEvidence.Documents {
		evidenceSize += len(document.JCS)
	}
	for _, occurrence := range fullEvidence.Occurrences {
		encoded, err := canonical.Marshal(occurrence)
		if err != nil {
			t.Fatal(err)
		}
		evidenceSize += len(encoded)
	}
	required := 1048576/evidenceSize + 1
	if required < 1 || required >= 5000 {
		t.Fatal("evidence fixture cannot exercise early bound", required)
	}
	oversized := make([]RenderDraftRequest, required+1)
	for i := range oversized {
		oversized[i] = loadedStack
	}
	counting := &renderCountingEvidenceResolver{CompositionInputResolver: resolver}
	probe := &renderCoordinatorProbe{}
	coordinator := RenderDraftCoordinator{Store: probe, Inputs: probe, Transport: probe, Resolver: counting}
	oversizedPlan, oversizedErr := coordinator.Prepare(ctx, uuid.New(), layerProject, layerSnapshot, strings.Repeat("a", 64), oversized)
	if !errors.Is(oversizedErr, v2input.ErrInvalid) || counting.calls != required || len(probe.events) != 0 || len(oversizedPlan.Tasks) != 0 || len(oversizedPlan.Config) != 0 {
		t.Fatal("oversized evidence continued or persisted", oversizedErr, counting.calls, required, probe.events)
	}
	loadedStack.LayerDocuments = loadedStack.LayerDocuments[:1]
	if _, _, e := CompileRenderDraft(layerPlan.Workflow, 0, loadedStack, layerEntries); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("direct task draft accepted incomplete layer proof", e)
	}
	if _, e := CompileRenderRunPlan(layerRun, layerProject, layerSnapshot, []RenderDraftRequest{loadedStack}, [][]v2input.Entry{layerEntries}); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("incomplete layer proof frozen", e)
	}
	draft, open, err := CompileRenderDraft(workflow, 0, request, entries)
	expectedTask, _ := v2input.TaskID(workflow, "render.compose", 0)
	if err != nil || draft.ID != expectedTask || draft.Operation != "render.compose" || draft.ResourceClass != "gpu" || draft.ParamsArtifactID != root.ArtifactID || draft.InputDigest == "" {
		t.Fatal("full render task draft invalid", draft, err)
	}
	cursor, err := open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		_, ok, err := cursor.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != len(entries) {
		t.Fatal("draft inventory incomplete", count)
	}
	if _, _, err := CompileRenderDraft(workflow, 0, request, entries[:1]); err == nil {
		t.Fatal("incomplete draft inventory accepted")
	}
	other, _, err := CompileRenderDraft(workflow, 1, request, entries)
	if err != nil || other.ID == draft.ID || other.InputDigest == draft.InputDigest {
		t.Fatal("render digest not bound to task ordinal", other, err)
	}
	for _, backend := range []string{"cpu", "cuda", "mps"} {
		alternate := request
		alternate.Backend = backend
		routed, _, err := CompileRenderDraft(workflow, 0, alternate, entries)
		if err != nil || routed.ResourceClass != "gpu" {
			t.Fatal("backend changed render routing class", backend, err)
		}
	}
	retry, _, err := CompileRenderDraft(workflow, 0, request, entries)
	if err != nil || retry.ID != draft.ID || retry.InputDigest != draft.InputDigest || !bytes.Equal(retry.ExecutionProfile, draft.ExecutionProfile) {
		t.Fatal("render draft retry changed", retry, err)
	}
	duplicate := append(append([]v2input.Entry(nil), entries...), entries[0])
	if _, _, err := CompileRenderDraft(workflow, 0, request, duplicate); err == nil {
		t.Fatal("duplicate render inventory accepted")
	}
	if _, _, err := CompileRenderDraft(uuid.Nil, 0, request, entries); err == nil {
		t.Fatal("missing render workflow accepted")
	}
	if _, err := CompileRenderRunPlan(uuid.New(), uuid.New(), uuid.Nil, []RenderDraftRequest{request}, [][]v2input.Entry{entries}); err == nil {
		t.Fatal("missing input snapshot accepted")
	}
	for _, failure := range []string{"", "begin", "publish", "register"} {
		probe := &renderCoordinatorProbe{fail: failure}
		coordinator := RenderDraftCoordinator{Store: probe, Inputs: probe, Transport: probe, Resolver: resolver}
		coordinated, err := coordinator.Prepare(ctx, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), []RenderDraftRequest{request, request})
		expected := []string{"begin", "publish", "register", "publish", "register"}
		switch failure {
		case "begin":
			expected = expected[:1]
		case "publish":
			expected = expected[:2]
		case "register":
			expected = expected[:3]
		}
		if strings.Join(probe.events, ",") != strings.Join(expected, ",") || (failure == "" && (err != nil || len(coordinated.Tasks) != 2)) || (failure != "" && (err == nil || len(coordinated.Tasks) != 2 || coordinated.ConfigSHA == "")) {
			t.Fatal("coordinator phase order/failure", failure, probe.events, err)
		}
	}
	for _, phase := range []string{"begin", "publish", "register"} {
		cancelCtx, cancel := context.WithCancel(ctx)
		probe := &renderCoordinatorProbe{cancelPhase: phase, cancel: cancel}
		coordinator := RenderDraftCoordinator{Store: probe, Inputs: probe, Transport: probe, Resolver: resolver}
		_, err := coordinator.Prepare(cancelCtx, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), []RenderDraftRequest{request, request})
		cancel()
		expected := map[string]string{"begin": "begin", "publish": "begin,publish", "register": "begin,publish,register"}[phase]
		if err != context.Canceled || strings.Join(probe.events, ",") != expected {
			t.Fatal("cancel crossed phase boundary", phase, probe.events, err)
		}
	}
	for _, retryPhase := range []string{"begin", "register"} {
		retryRun, retryProject, retrySnapshot := uuid.New(), uuid.New(), uuid.New()
		probe := &renderCoordinatorProbe{fail: retryPhase}
		coordinator := RenderDraftCoordinator{Store: probe, Inputs: probe, Transport: probe, Resolver: resolver}
		interrupted, err := coordinator.Prepare(ctx, retryRun, retryProject, retrySnapshot, strings.Repeat("a", 64), []RenderDraftRequest{request, request})
		if err == nil {
			t.Fatal("interrupted persistence unexpectedly succeeded")
		}
		probe.fail = ""
		recovered, err := coordinator.Prepare(ctx, retryRun, retryProject, retrySnapshot, strings.Repeat("a", 64), []RenderDraftRequest{request, request})
		if err != nil || interrupted.ConfigSHA != recovered.ConfigSHA || interrupted.Tasks[0].ID != recovered.Tasks[0].ID || interrupted.Tasks[0].InputDigest != recovered.Tasks[0].InputDigest {
			t.Fatal("retry changed frozen plan", err)
		}
	}
	snapshot := uuid.New()
	recoveryRun, recoveryProject := uuid.New(), uuid.New()
	runPlan, err := CompileRenderRunPlan(recoveryRun, recoveryProject, snapshot, []RenderDraftRequest{request, request}, [][]v2input.Entry{entries, entries})
	if err != nil || len(runPlan.Tasks) != 2 || runPlan.Tasks[0].ID == runPlan.Tasks[1].ID || runPlan.OutputBytes < 1 || runPlan.ConfigSHA == "" {
		t.Fatal("run plan compilation failed", runPlan, err)
	}
	saved := RecoveredRenderDraft{RunID: recoveryRun, WorkflowID: runPlan.Workflow, ProjectID: recoveryProject, SnapshotID: snapshot, Config: runPlan.Config, ConfigSHA: runPlan.ConfigSHA, ReservedBytes: runPlan.OutputBytes}
 var queuedConfig map[string]json.RawMessage
 if err:=json.Unmarshal(runPlan.Config,&queuedConfig);err!=nil||string(queuedConfig["queue_class"])!=`"batch"`{t.Fatal("render batch queue not frozen",err)}
 interactivePlan,queueErr:=compileRenderRunPlanForQueue(recoveryRun,recoveryProject,snapshot,[]RenderDraftRequest{request,request},[][]v2input.Entry{entries,entries},"interactive")
 if queueErr!=nil||interactivePlan.ConfigSHA==runPlan.ConfigSHA||interactivePlan.Tasks[0].ID!=runPlan.Tasks[0].ID||interactivePlan.Tasks[0].InputDigest!=runPlan.Tasks[0].InputDigest{t.Fatal("queue changed task identity or was not hashed",queueErr)}
 wrongQueue:=saved
 queuedConfig["queue_class"]=json.RawMessage(`"training"`)
 wrongQueue.Config,_=canonical.Marshal(queuedConfig)
 wrongQueue.ConfigSHA,_=canonical.Hash(wrongQueue.Config)
 if _,err:=DecodeRecoveredRenderPlan(wrongQueue);!errors.Is(err,v2input.ErrInvalid){t.Fatal("foreign render queue accepted",err)}
	decoded, err := DecodeRecoveredRenderPlan(saved)
	if err != nil || len(decoded) != 2 || decoded[0].Node.ParamsJCS != string(recipe) || decoded[0].Node.ProtectionJCS != string(protectionRaw) {
		t.Fatal("recovered evidence invalid", err)
	}
	batchID := uuid.New()
	batchPlans := make([]CompiledRenderBatchPart, 2)
	batchSaved := make([]RecoveredRenderDraft, 2)
	for i := range batchPlans {
		child := uuid.NewSHA1(batchID, []byte("render.batch:"+strconv.Itoa(i)))
		compiled, err := CompileRenderRunPlan(child, recoveryProject, snapshot, []RenderDraftRequest{request}, [][]v2input.Entry{entries})
		if err != nil {
			t.Fatal(err)
		}
		batchPlans[i] = CompiledRenderBatchPart{RunID: child, FirstOrdinal: i, Plan: compiled}
		batchSaved[i] = RecoveredRenderDraft{RunID: child, WorkflowID: compiled.Workflow, ProjectID: recoveryProject, SnapshotID: snapshot, Config: compiled.Config, ConfigSHA: compiled.ConfigSHA, PolicySHA: strings.Repeat("a", 64), ReservedBytes: compiled.OutputBytes}
	}
	manifestRaw, err := BuildRenderBatchManifest(batchID, recoveryProject, snapshot, actor, strings.Repeat("a", 64), batchPlans)
	if err != nil {
		t.Fatal(err)
	}
	manifestSHA, _ := canonical.Hash(json.RawMessage(manifestRaw))
	manifest, err := DecodeRenderBatchManifest(batchID, actor.SessionID, manifestRaw, manifestSHA)
	if err != nil {
		t.Fatal(err)
	}
	batchStore := &renderBatchRecoveryProbe{parent: RecoveredRenderBatch{BatchID: batchID, Manifest: manifest, ManifestSHA: manifestSHA}, parts: batchSaved, failPart: -1}
	batchPublication := &renderCoordinatorProbe{}
	batchCoordinator := RenderBatchCoordinator{Store: batchStore, Inputs: RenderDraftCoordinator{Inputs: batchPublication, Transport: batchPublication, Resolver: resolver}}
	if err := batchCoordinator.RecoverInputs(ctx, tenant, batchID, actor); err != nil || strings.Join(batchPublication.events, ",") != "publish,register,publish,register" {
		t.Fatal("full typed batch recovery failed", err, batchPublication.events)
	}
	batchPublication.events = nil
	batchStore.failPart = 1
	if err := batchCoordinator.RecoverInputs(ctx, tenant, batchID, actor); err == nil || len(batchPublication.events) != 0 {
		t.Fatal("late missing child published an earlier child")
	}
	batchStore.failPart = -1
	oldSHA := batchStore.parts[1].ConfigSHA
	batchStore.parts[1].ConfigSHA = strings.Repeat("f", 64)
	if err := batchCoordinator.RecoverInputs(ctx, tenant, batchID, actor); err == nil || len(batchPublication.events) != 0 {
		t.Fatal("late mismatched child published an earlier child")
	}
	batchStore.parts[1].ConfigSHA = oldSHA
	batchCoordinator.Inputs.Resolver = &renderBatchLateDrift{entries: entries}
	if err := batchCoordinator.RecoverInputs(ctx, tenant, batchID, actor); err == nil || len(batchPublication.events) != 0 {
		t.Fatal("late inventory drift published an earlier child")
	}
	batchCoordinator.Inputs.Resolver = resolver
	batchStore.parent.CancelRequested = true
	if err := batchCoordinator.RecoverInputs(ctx, tenant, batchID, actor); err == nil || len(batchPublication.events) != 0 {
		t.Fatal("cancelled batch published inputs")
	}
	batchStore.failBegin = true
	preparedBatch, err := batchCoordinator.Prepare(ctx, batchID, recoveryProject, snapshot, strings.Repeat("a", 64), []RenderDraftRequest{request, request})
	if err == nil || len(preparedBatch) == 0 || len(batchPublication.events) != 0 {
		t.Fatal("failed batch begin lost reconciliation evidence or published inputs")
	}
	batchStore.failBegin = false
	retriedBatch, err := batchCoordinator.Prepare(ctx, batchID, recoveryProject, snapshot, strings.Repeat("a", 64), []RenderDraftRequest{request, request})
	if err != nil || retriedBatch[0].Plan.ConfigSHA != preparedBatch[0].Plan.ConfigSHA || strings.Join(batchPublication.events, ",") != "publish,register,publish,register" {
		t.Fatal("prepared batch retry invalid", err)
	}
	batchPublication.events = nil
	cancelBatchCtx, cancelBatch := context.WithCancel(ctx)
	batchStore.cancelBegin = cancelBatch
	cancelledPlans, err := batchCoordinator.Prepare(cancelBatchCtx, uuid.New(), recoveryProject, snapshot, strings.Repeat("a", 64), []RenderDraftRequest{request})
	if !errors.Is(err, context.Canceled) || len(cancelledPlans) == 0 || len(batchPublication.events) != 0 {
		t.Fatal("batch published after cancellation at commit boundary", err)
	}
	batchStore.cancelBegin = nil
	recoveryProbe := &renderCoordinatorProbe{}
	recoveryCoordinator := RenderDraftCoordinator{Inputs: recoveryProbe, Transport: recoveryProbe, Resolver: resolver}
	if err := recoveryCoordinator.RecoverInputs(ctx, tenant, actor, saved); err != nil || strings.Join(recoveryProbe.events, ",") != "publish,register,publish,register" {
		t.Fatal("input recovery failed", recoveryProbe.events, err)
	}
	recoveryProbe.events = nil
	foreignActor := actor
	foreignActor.SessionID = uuid.New()
	if err := recoveryCoordinator.RecoverInputs(ctx, tenant, foreignActor, saved); err == nil || len(recoveryProbe.events) != 0 {
		t.Fatal("foreign session recovered inputs")
	}
	drift := append([]v2input.Entry(nil), entries...)
	drift = append(drift, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "Artifact"})
	recoveryCoordinator.Resolver = renderResolvedInventory{drift}
	if err := recoveryCoordinator.RecoverInputs(ctx, tenant, actor, saved); err == nil || len(recoveryProbe.events) != 0 {
		t.Fatal("changed inventory digest published on recovery")
	}
	recoveryCoordinator.Resolver = renderResolvedInventory{entries[:1]}
	if err := recoveryCoordinator.RecoverInputs(ctx, tenant, actor, saved); err == nil || len(recoveryProbe.events) != 0 {
		t.Fatal("incomplete inventory published on recovery")
	}
	underfunded := saved
	underfunded.ReservedBytes--
	if _, err := DecodeRecoveredRenderPlan(underfunded); err == nil {
		t.Fatal("underfunded recovery accepted")
	}
	foreignRecovery := saved
	foreignRecovery.SnapshotID = uuid.New()
	if _, err := DecodeRecoveredRenderPlan(foreignRecovery); err == nil {
		t.Fatal("foreign snapshot recovery accepted")
	}
	var config struct {
		Snapshot uuid.UUID `json:"input_snapshot_id"`
		Render   struct {
			Nodes []RenderRunNode `json:"nodes"`
		} `json:"v26_render"`
	}
	if json.Unmarshal(runPlan.Config, &config) != nil || config.Snapshot != snapshot || len(config.Render.Nodes) != 2 || config.Render.Nodes[0].SourceJCS != string(source) || config.Render.Nodes[0].ParamsJCS != string(recipe) {
		t.Fatal("run evidence lost")
	}
	if protected {
		var altered map[string]any
		if err := json.Unmarshal(saved.Config, &altered); err != nil {
			t.Fatal(err)
		}
		node := altered["v26_render"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
		delete(node, "protection_jcs")
		changed := saved
		changed.Config, err = canonical.Marshal(altered)
		if err != nil {
			t.Fatal(err)
		}
		changed.ConfigSHA, err = canonical.Hash(json.RawMessage(changed.Config))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeRecoveredRenderPlan(changed); err == nil {
			t.Fatal("rehashed config lost mandatory protection evidence without rejection")
		}
	}
	if !protected {
		large := make([]RenderDraftRequest, 5000)
		inventories := make([][]v2input.Entry, len(large))
		for i := range large {
			large[i], inventories[i] = request, entries
		}
		if _, err := CompileRenderRunPlan(uuid.New(), uuid.New(), snapshot, large, inventories); err == nil {
			t.Fatal("oversized run config cannot be persisted or recovered")
		}
		batch, project := uuid.New(), uuid.New()
		parts, err := CompileRenderBatchPlans(batch, project, snapshot, large, inventories)
		if err != nil || len(parts) < 2 {
			t.Fatal("large selection not partitioned", err)
		}
		covered := 0
		for index, part := range parts {
			if part.FirstOrdinal != covered || len(part.Plan.Config) > 1048576 || len(part.Plan.Tasks) == 0 {
				t.Fatal("invalid batch partition")
			}
			if part.RunID != uuid.NewSHA1(batch, []byte("render.batch:"+strconv.Itoa(index))) {
				t.Fatal("unstable batch run identity")
			}
			recovered := RecoveredRenderDraft{RunID: part.RunID, WorkflowID: part.Plan.Workflow, ProjectID: project, SnapshotID: snapshot, Config: part.Plan.Config, ConfigSHA: part.Plan.ConfigSHA, ReservedBytes: part.Plan.OutputBytes}
			if _, err := DecodeRecoveredRenderPlan(recovered); err != nil {
				t.Fatal("partition cannot be recovered", err)
			}
			covered += len(part.Plan.Tasks)
		}
		if covered != 5000 {
			t.Fatal("batch lost selected frames", covered)
		}
		manifestRaw, err := BuildRenderBatchManifest(batch, project, snapshot, actor, strings.Repeat("a", 64), parts)
		if err != nil || len(manifestRaw) > 1048576 {
			t.Fatal("batch manifest invalid", err)
		}
		var manifest RenderBatchManifest
		if json.Unmarshal(manifestRaw, &manifest) != nil || len(manifest.Parts) != len(parts) {
			t.Fatal("batch manifest lost part evidence")
		}
		manifest.Parts[0].FirstOrdinal++
		if validateRenderBatchManifest(batch, manifest) == nil {
			t.Fatal("batch manifest gap accepted")
		}
		alteredParts := append([]CompiledRenderBatchPart(nil), parts...)
		alteredParts[0].Plan.ConfigSHA = strings.Repeat("f", 64)
		if _, err := BuildRenderBatchManifest(batch, project, snapshot, actor, strings.Repeat("a", 64), alteredParts); err == nil {
			t.Fatal("corrupt child pinned in batch manifest")
		}
	}
	foreign := request
	foreign.Tenant = uuid.New()
	if _, err := CompileRenderRunPlan(uuid.New(), uuid.New(), snapshot, []RenderDraftRequest{request, foreign}, [][]v2input.Entry{entries, entries}); err == nil {
		t.Fatal("mixed tenant run accepted")
	}
	publisher := &compositionTransportPublisher{}
	graph, err := PrepareRenderInputClosure(ctx, actor, tenant, uuid.New(), binding, root, recipe, source, resolver, publisher)
	if err != nil || len(graph.Direct) != 1 || graph.Direct[0] != root || graph.Built.Manifest.ArtifactID == uuid.Nil {
		t.Fatal("full render transport failed", graph, err)
	}
	for pass := 0; pass < 2; pass++ {
		cursor, err := graph.Open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			_, ok, err := cursor.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			count++
		}
		if count != len(entries) {
			t.Fatal("retry inventory changed", count)
		}
	}
	documents := &renderScopedDocuments{bodies: map[string][]byte{}, failManifest: true}
	scoped := RenderTransportPublisher{Documents: documents}
	run, task := uuid.New(), uuid.New()
	if _, err := scoped.Prepare(ctx, actor, tenant, run, task, binding, root, recipe, source, resolver); !errors.Is(err, errRenderManifestTransient) {
		t.Fatal("manifest failure not propagated", err)
	}
	retried, err := scoped.Prepare(ctx, actor, tenant, run, task, binding, root, recipe, source, resolver)
	if err != nil || retried.ID != task || retried.Direct[0] != root {
		t.Fatal("scoped retry failed", retried, err)
	}
	for _, scope := range documents.scopes {
		if scope.Tenant != tenant || scope.Run != run || scope.Owner != task {
			t.Fatal("transport scope changed", scope)
		}
	}
	if len(documents.bodies) != 2 {
		t.Fatal("unexpected scoped transport", len(documents.bodies))
	}
	store.bodies[style.ArtifactID.String()] = []byte("{}")
	before := publisher.calls
	if _, err := PrepareRenderInputClosure(ctx, actor, tenant, uuid.New(), binding, root, recipe, source, resolver, publisher); err == nil || publisher.calls != before {
		t.Fatal("invalid closure published transport", err)
	}
	if out, err := ResolveRenderInputs(ctx, actor, tenant, binding, root, recipe, source, resolver); err == nil || out != nil {
		t.Fatal("corrupt descendant returned inputs", out, err)
	}
}

var errRenderManifestTransient = errors.New("test manifest publication interrupted")

type renderScopedDocuments struct {
	bodies       map[string][]byte
	scopes       []artifactjson.Scope
	failManifest bool
}

func (p *renderScopedDocuments) Publish(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	p.scopes = append(p.scopes, scope)
	if scope.Purpose == "v2_input_manifest" && p.failManifest {
		p.failManifest = false
		return artifactjson.Ref{}, errRenderManifestTransient
	}
	if previous, ok := p.bodies[scope.Purpose]; ok && !bytes.Equal(previous, body) {
		return artifactjson.Ref{}, errors.New("retry changed immutable document")
	}
	p.bodies[scope.Purpose] = append([]byte(nil), body...)
	sum := sha256.Sum256(body)
	return artifactjson.Ref{ID: uuid.NewSHA1(scope.Owner, []byte(scope.Purpose)), SHA256: hex.EncodeToString(sum[:])}, nil
}

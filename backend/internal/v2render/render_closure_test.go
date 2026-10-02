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

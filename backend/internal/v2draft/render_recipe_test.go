package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestFrozenRenderRecipePreservesCompositionAndChecksLayerBytes(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	sum := sha256.Sum256(stack)
	layer := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": layer, "protected_mask": protection, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{EV: .1}})
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("e", 64), ModelLockSHA: strings.Repeat("f", 64)}
	out, err := BuildFrozenRenderRecipe(raw, inputs, stack)
	if err != nil {
		t.Fatal(err)
	}
	// Wire/version validation must precede the upload dependency boundary.
	publisher := Publisher{}
	registrationSum := sha256.Sum256(out)
	registrationRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(registrationSum[:])}
	registrationActor := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if err := publisher.RegisterFrozenRenderRecipe(context.Background(), registrationActor, uuid.New(), binding.AssetVersionID, uuid.New(), registrationRef, out); err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid registration did not reach dependency gate", err)
	}
	changedRef := registrationRef
	changedRef.SHA256 = strings.Repeat("0", 64)
	if err := publisher.RegisterFrozenRenderRecipe(context.Background(), registrationActor, uuid.New(), binding.AssetVersionID, uuid.New(), changedRef, out); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatal("changed registration pin reached database", err)
	}
	if err := publisher.RegisterFrozenRenderRecipe(context.Background(), registrationActor, uuid.New(), uuid.New(), uuid.New(), registrationRef, out); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatal("foreign registration version reached database", err)
	}
	_, compiledPublishErr := (Publisher{}).CompileAndPublishFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), raw, inputs, stack)
	if compiledPublishErr == nil || !strings.Contains(compiledPublishErr.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("compiled document did not reach upload boundary", compiledPublishErr)
	}
	for _, badLayer := range [][]byte{nil, append(append([]byte(nil), stack...), ' ')} {
		_, err := (Publisher{}).CompileAndPublishFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), raw, inputs, badLayer)
		if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("unverified authored layer reached upload", err)
		}
	}
	for _, invalid := range []struct {
		version uuid.UUID
		body    []byte
	}{{uuid.New(), out}, {binding.AssetVersionID, append(append([]byte(nil), out...), ' ')}, {binding.AssetVersionID, []byte(`{}`)}} {
		_, err := publisher.PublishFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), invalid.version, uuid.New(), invalid.body)
		if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
			t.Fatal("invalid recipe reached upload dependency", err)
		}
	}
	_, publishErr := publisher.PublishFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), binding.AssetVersionID, uuid.New(), out)
	if publishErr == nil || !strings.Contains(publishErr.Error(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("valid recipe did not reach publisher dependency check", publishErr)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publisher.PublishFrozenRenderRecipe(canceled, auth.Identity{}, uuid.New(), binding.AssetVersionID, uuid.New(), out); err != context.Canceled {
		t.Fatal("canceled publication continued", err)
	}
	if python := os.Getenv("PHOTO_WORKER_PYTHON"); python != "" {
		root, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(python, "-c", "import sys; from photo_worker.v2.render_recipe import decode_frozen_render_recipe; from photo_worker.contracts.base import canonical_bytes; raw=sys.stdin.buffer.read(); assert canonical_bytes(decode_frozen_render_recipe(raw))==raw")
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers")+string(os.PathListSeparator)+filepath.Join(root, "apps", "local-editor", "src"))
		command.Stdin = bytes.NewReader(out)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("worker rejected actual Go recipe: %v %s", err, output)
		}
	}
	rootRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("9", 64)}
	children, refsErr := DocumentChildren(out, "FrozenRenderRecipe", rootRef)
	if refsErr != nil || len(children) != 4 {
		t.Fatal(children, refsErr)
	}
	for i, want := range []string{"WorkingImage", "StyleV2", "LayerStack", "MaskRef"} {
		if children[i].Contract != want || children[i].ParentRef != rootRef {
			t.Fatal(children)
		}
	}
	if children[0].CanvasHash != binding.CanvasHash || children[2].Ref != layer || children[3].Ref != protection {
		t.Fatal(children)
	}
	decoded, decodeErr := DecodeFrozenRenderRecipe(out)
	if decodeErr != nil || decoded.RecipeSHA256 == "" {
		t.Fatal(decoded, decodeErr)
	}
	corrupt := append([]byte(nil), out...)
	corrupt = bytes.Replace(corrupt, []byte(`"recipe_sha256":"`), []byte(`"unknown":"x","recipe_sha256":"`), 1)
	if _, err := DecodeFrozenRenderRecipe(corrupt); err == nil {
		t.Fatal("unknown field accepted")
	}
	var frozen FrozenRenderRecipe
	if json.Unmarshal(out, &frozen) != nil || string(frozen.Recipe) != string(raw) || frozen.Scalars.EffectiveDelta.EV != .1 || frozen.Learning != nil {
		t.Fatal("authored composition lost")
	}
	recipeSum := sha256.Sum256(raw)
	if frozen.RecipeSHA256 != hex.EncodeToString(recipeSum[:]) {
		t.Fatal("recipe provenance changed")
	}
	for _, bad := range [][]byte{nil, append(append([]byte(nil), stack...), ' ')} {
		if _, err := BuildFrozenRenderRecipe(raw, inputs, bad); err == nil {
			t.Fatal("unverified layer body accepted")
		}
	}
	if _, err := BuildFrozenRenderRecipe(append(raw, ' '), inputs, stack); err == nil {
		t.Fatal("noncanonical recipe accepted")
	}
	inputs.LearningModelRef = style
	if _, err := BuildFrozenRenderRecipe(raw, inputs, stack); err == nil {
		t.Fatal("unexpected learning dependency accepted")
	}
}

func TestFrozenRenderRecipeLearningDependenciesAndTampering(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	style := ref()
	learningID := uuid.New()
	delta := CorrectionVector{EV: .1}
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": learningID, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, LearningID: learningID, Learning: &delta, LearningModelRef: ref(), LearningDescriptorRef: ref(), LearningAnalysisID: uuid.New(), LearningDescriptorOrdinal: 4999, ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}
	wire, err := BuildFrozenRenderRecipe(raw, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := ref()
	children, err := DocumentChildren(wire, "FrozenRenderRecipe", parent)
	if err != nil || len(children) != 4 || children[2].Contract != "CandidateProfile" || children[2].Ref != inputs.LearningModelRef || children[3].Contract != "DescriptorShard" || children[3].Ref != inputs.LearningDescriptorRef {
		t.Fatal(children, err)
	}
	for _, child := range children[2:] {
		if child.ExpectedLearning == nil || child.ExpectedBinding == nil || *child.ExpectedBinding != binding || child.ExpectedLearning.VersionID != learningID || child.ExpectedLearning.AnalysisID != inputs.LearningAnalysisID || child.ExpectedLearning.Ordinal != 4999 {
			t.Fatal("learning occurrence lost frozen authority", child)
		}
	}
	original, err := DecodeFrozenRenderRecipe(wire)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*FrozenRenderRecipe){
		func(f *FrozenRenderRecipe) { f.Learning.VersionID = uuid.New() },
		func(f *FrozenRenderRecipe) { f.Learning.Ordinal = 5000 },
		func(f *FrozenRenderRecipe) { f.Learning.Model.ArtifactID = uuid.Nil },
		func(f *FrozenRenderRecipe) { f.Learning = nil },
		func(f *FrozenRenderRecipe) { f.Scalars.BaseStyle = ref() },
		func(f *FrozenRenderRecipe) { f.Scalars.Source.Source = ref() },
		func(f *FrozenRenderRecipe) { f.RecipeSHA256 = strings.Repeat("f", 64) },
		func(f *FrozenRenderRecipe) { f.Scalars.EffectiveDelta.EV = 3 },
	}
	for i, mutate := range mutations {
		copy := original
		learning := *original.Learning
		copy.Learning = &learning
		mutate(&copy)
		bad, _ := canonical.Marshal(copy)
		if _, err := DocumentChildren(bad, "FrozenRenderRecipe", parent); err == nil {
			t.Fatal("tamper accepted", i)
		}
	}
}

func TestFrozenRenderClosureRejectsWrongTaskSourceBeforeChildIO(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	style := ref()
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	body, err := BuildFrozenRenderRecipe(raw, ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	fixture := &imageClosureCatalog{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{root.ArtifactID: {Key: "root", SHA256: root.SHA256, Bytes: int64(len(body)), VersionID: "v1", ContentType: "application/json"}}, bodies: map[string][]byte{"root": body}}}
	wrong := binding
	wrong.AssetVersionID = uuid.New()
	resolver := ClosureResolver{Catalog: fixture, Objects: fixture}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), []LayerRef{{Contract: "FrozenRenderRecipe", Ref: root, Path: "root"}}, wrong); err == nil {
		t.Fatal("different task source accepted")
	}
	if len(fixture.lookups) != 2 || fixture.lookups[0].Ref != root || fixture.lookups[1].Ref != root || fixture.reads != 1 {
		t.Fatal("child IO preceded task source proof", fixture.lookups)
	}
}

func TestFrozenRenderRecipeDependenciesUseExactTypedAdmissionRoots(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	sum := sha256.Sum256(stack)
	layers := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": layers, "protected_mask": protection, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	raw, err := BuildFrozenRenderRecipe(authored, ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("e", 64), ModelLockSHA: strings.Repeat("f", 64)}, stack)
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(raw)
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	roots, err := FrozenRenderRecipeDependencies(raw, parent)
	if err != nil || len(roots) != 4 {
		t.Fatal("typed dependency inventory", err, len(roots))
	}
	expected := []struct {
		contract string
		ref      v2input.Ref
	}{{"WorkingImage", binding.Source}, {"StyleV2", style}, {"LayerStack", layers}, {"MaskRef", protection}}
	for i, want := range expected {
		if roots[i].Contract != want.contract || roots[i].Ref != want.ref || roots[i].ParentRef != parent {
			t.Fatal("dependency changed", i, roots[i])
		}
	}
	if roots[0].CanvasHash != binding.CanvasHash {
		t.Fatal("source canvas binding omitted")
	}
	bad := parent
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := FrozenRenderRecipeDependencies(raw, bad); err == nil {
		t.Fatal("foreign parent accepted")
	}
	raw[0] = 'x'
	if roots[0].Ref != binding.Source {
		t.Fatal("dependency inventory aliases input bytes")
	}
	if _, err := FrozenRenderRecipeDependencies(raw, parent); err == nil {
		t.Fatal("changed bytes accepted")
	}
}

package v2render

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
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestFullRenderEvidencePreservesAuthoredCompositionAndExactBinding(t *testing.T) {
	sha := strings.Repeat("a", 64)
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	canvas := map[string]any{"width": 2000, "height": 1000, "orientation": 1, "space_id": "source", "source_sha256": sha}
	chainSHA, _ := canonical.Hash(map[string]any{"steps": []any{}})
	source, _ := canonical.Marshal(map[string]any{"pixels": v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, "alpha": nil, "dtype": "float32", "canvas": canvas, "color": map[string]any{"profile_id": "linear_srgb_v1", "encoding": "linear", "icc_sha256": sha}, "transform_chain": map[string]any{"steps": []any{}, "hash": chainSHA}})
	canvasSHA, _ := canonical.Hash(canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(source)}, CanvasHash: canvasSHA}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	stackRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(stack)}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": stackRef, "protected_mask": protection, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{EV: .1}})
	recipe, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: sha, ModelLockSHA: sha}, stack)
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(recipe)}
	budget, err := CompileRenderBudget(binding, root, recipe, source, stack, 512, "require_float32", RenderProviderPeak{VRAMBytes: 123})
	if err != nil || budget.Resources.PixelCount != 2000000 || budget.MaskRefs != 1 || budget.MaskNodes != 1 || budget.Resources.VRAMBytes != 123 {
		t.Fatal("pinned render budget invalid", budget, err)
	}
	for _, badLayer := range [][]byte{nil, []byte(`{}`), append(append([]byte(nil), stack...), ' ')} {
		if _, err := CompileRenderBudget(binding, root, recipe, source, badLayer, 512, "require_float32", RenderProviderPeak{}); err == nil {
			t.Fatal("changed layer bytes budgeted")
		}
	}
	profile, err := CompileRenderExecutionProfile(binding, root, recipe, source, stack, "cuda", sha, 512, "require_float32", RenderProviderPeak{VRAMBytes: 123})
	var execution struct {
		Backend   string                `json:"backend"`
		Resources LayerResourceEstimate `json:"resource_estimate"`
		ModelSHA  string                `json:"model_lock_sha"`
	}
	if err != nil || json.Unmarshal(profile, &execution) != nil || execution.Backend != "cuda" || execution.Resources != budget.Resources || execution.ModelSHA != sha {
		t.Fatal("full render profile differs from verified budget", string(profile), err)
	}
	if python := os.Getenv("PHOTO_WORKER_PYTHON"); python != "" {
		repo, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(python, "-c", `import json,sys
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_resources import validate_render_execution_profile,admit_render
wire=json.load(sys.stdin)
recipe=decode_frozen_render_recipe(wire['recipe'].encode())
profile=json.loads(wire['profile'])
validate_render_execution_profile(profile,recipe,backend='cuda',runtime_sha='a'*64)
admit_render(profile,2000,1000,mask_refs=1,mask_nodes=1,provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=123,output_bytes=0))
print('admitted')`)
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repo, "workers")+string(os.PathListSeparator)+filepath.Join(repo, "src"))
		wire, _ := json.Marshal(map[string]string{"recipe": string(recipe), "profile": string(profile)})
		command.Stdin = bytes.NewReader(wire)
		output, err := command.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "admitted" {
			t.Fatal("actual worker rejected Go profile", err, string(output))
		}
	}
	if _, err := CompileRenderExecutionProfile(binding, root, recipe, source, stack, "unknown", sha, 512, "require_float32", RenderProviderPeak{}); err == nil {
		t.Fatal("unsupported backend admitted")
	}
	result, err := CompileRenderEvidence(binding, root, recipe, source)
	if err != nil || string(result.Recipe.Recipe) != string(authored) || result.Source.Canvas.Width != 2000 || result.RecipeInput.Contract != "FrozenRenderRecipe" || result.SourceInput.Ref != binding.Source {
		t.Fatal(result, err)
	}
	roots, err := RenderInputRoots(binding, root, recipe, source)
	if err != nil || len(roots) != 2 {
		t.Fatal(roots, err)
	}
	contracts := map[string]v2input.Ref{}
	for _, entry := range roots {
		contracts[entry.Contract] = entry.Ref
	}
	if contracts["FrozenRenderRecipe"] != root || contracts["WorkingImage"] != binding.Source {
		t.Fatal(roots)
	}
	collision := root
	collision.ArtifactID = binding.Source.ArtifactID
	if _, err := RenderInputRoots(binding, collision, recipe, source); err == nil {
		t.Fatal("conflicting root roles accepted")
	}
	if _, err := RenderInputRoots(binding, root, append(append([]byte(nil), recipe...), ' '), source); err == nil {
		t.Fatal("unverified root forwarded to resolver")
	}
	actor := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	inventory := append([]v2input.Entry(nil), roots...)
	seen := map[uuid.UUID]bool{}
	for _, entry := range inventory {
		seen[entry.Ref.ArtifactID] = true
	}
	for _, doc := range []struct {
		raw      []byte
		contract string
		ref      v2input.Ref
	}{{recipe, "FrozenRenderRecipe", root}, {source, "WorkingImage", binding.Source}} {
		children, err := v2draft.DocumentChildren(doc.raw, doc.contract, doc.ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range children {
			if !seen[child.Ref.ArtifactID] {
				contract := child.Contract
				if contract == "WorkingPixels" || contract == "WorkingAlpha" {
					contract = "NpyArray"
				}
				inventory = append(inventory, v2input.Entry{Ref: child.Ref, Contract: contract})
				seen[child.Ref.ArtifactID] = true
			}
		}
	}
	resolver := &renderResolverFixture{entries: inventory}
	resolved, err := ResolveRenderInputs(context.Background(), actor, tenant, binding, root, recipe, source, resolver)
	if err != nil || len(resolved) != len(inventory) || resolver.calls != 1 {
		t.Fatal(resolved, err)
	}
	for _, entry := range resolved {
		if entry.Contract == "NpyArray" {
			bad := append([]v2input.Entry(nil), resolved...)
			for i := range bad {
				if bad[i].Ref == entry.Ref {
					bad[i].Contract = "WorkingPixels"
				}
			}
			resolver.entries = bad
			if _, err := ResolveRenderInputs(context.Background(), actor, tenant, binding, root, recipe, source, resolver); err == nil {
				t.Fatal("nontransport pixel contract accepted")
			}
			break
		}
	}
	inventory[0].Contract = "changed"
	for _, entry := range resolved {
		if entry.Contract == "changed" {
			t.Fatal("resolver slice not frozen")
		}
	}
	for _, bad := range [][]v2input.Entry{roots, append(append([]v2input.Entry(nil), resolved...), resolved[0])} {
		resolver.entries = bad
		if _, err := ResolveRenderInputs(context.Background(), actor, tenant, binding, root, recipe, source, resolver); err == nil {
			t.Fatal("incomplete or duplicate resolver inventory accepted")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	beforeCalls := resolver.calls
	if _, err := ResolveRenderInputs(canceled, actor, tenant, binding, root, recipe, source, resolver); err == nil || resolver.calls != beforeCalls {
		t.Fatal("canceled request reached resolver")
	}
	wrongVersion := binding
	wrongVersion.AssetVersionID = uuid.New()
	wrongCanvas := binding
	wrongCanvas.CanvasHash = sha
	wrongSource := binding
	wrongSource.Source.ArtifactID = uuid.New()
	for _, wrong := range []v2input.SessionVersionBinding{wrongVersion, wrongCanvas, wrongSource} {
		if _, err := CompileRenderEvidence(wrong, root, recipe, source); err == nil {
			t.Fatal("changed source binding accepted")
		}
	}
	for _, raw := range [][]byte{append(append([]byte(nil), recipe...), ' '), []byte(`{}`), nil} {
		ref := root
		ref.SHA256 = hash(raw)
		if _, err := CompileRenderEvidence(binding, ref, raw, source); err == nil {
			t.Fatal("invalid full recipe accepted")
		}
	}
	if _, err := CompileRenderEvidence(binding, root, recipe, append(append([]byte(nil), source...), ' ')); err == nil {
		t.Fatal("changed source bytes accepted")
	}
}

type renderResolverFixture struct {
	entries []v2input.Entry
	calls   int
}

func (r *renderResolverFixture) ResolveTaskInputs(_ context.Context, _ auth.Identity, _ uuid.UUID, _ []v2input.Entry, _ v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	r.calls++
	return r.entries, nil
}

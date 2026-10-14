package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFullRecipeClosure128Frames(t *testing.T) {
	t.Run("shared", func(t *testing.T) { testFullRecipeClosure128Frames(t, false) })
	t.Run("distinct", func(t *testing.T) { testFullRecipeClosure128Frames(t, true) })
}

func testFullRecipeClosure128Frames(t *testing.T, distinct bool) {
	f := &imageClosureCatalog{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}}
	put := func(value any) v2input.Ref {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		key := ref.ArtifactID.String()
		f.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256, Bytes: int64(len(body)), VersionID: "v1", ContentType: "application/json"}
		f.bodies[key] = body
		return ref
	}
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	json.Unmarshal(raw, &images)
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	source := put(images[0])
	canvas, _ := canonical.Hash(image.Canvas)
	f.objects[image.Pixels.ID] = objectstore.Object{Key: "pixels", SHA256: image.Pixels.SHA256, Bytes: 10, VersionID: "v1", ContentType: "application/x-npy"}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}
	styleRaw, err := os.ReadFile("../../../docs/implementation/contracts/examples/style-fixed.example.json")
	if err != nil {
		t.Fatal(err)
	}
	style := put(json.RawMessage(styleRaw))
	frames := []any{}
	for i := 0; i < 128; i++ {
		itemBinding := binding
		if i > 0 {
			itemBinding.AssetVersionID = uuid.New()
			if distinct {
				var variant map[string]any
				if json.Unmarshal(images[0], &variant) != nil {
					t.Fatal("fixture")
				}
				pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: image.Pixels.SHA256}
				variant["pixels"] = pixels
				itemBinding.Source = put(variant)
				f.objects[pixels.ArtifactID] = objectstore.Object{Key: pixels.ArtifactID.String(), SHA256: pixels.SHA256, Bytes: 10, VersionID: "v1", ContentType: "application/x-npy"}
			}

		}
		faces := put(map[string]any{"schema_version": 1, "binding": itemBinding, "faces": []any{}})
		frames = append(frames, map[string]any{"schema_version": 1, "binding": itemBinding, "ordinal": i, "captured_at": nil, "camera_hash": nil, "scene_embedding": make([]float64, 384), "luma_quantiles": []float64{.1, .2, .3, .4, .5}, "neutral_rgb": nil, "sharpness": 1, "clipping_fraction": 0, "person_fraction": 0, "faces": faces})
	}
	shard := put(map[string]any{"schema_version": 1, "snapshot_digest": strings.Repeat("a", 64), "frames": frames})
	model := CandidateProfile{SchemaVersion: 1, ProfileID: uuid.New(), TrainingSHA: strings.Repeat("b", 64), FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range model.Scales {
		model.Scales[i] = 1
	}
	modelRef := put(model)
	learningID := uuid.New()
	delta := CorrectionVector{}
	corrections := put(map[string]any{"schema_version": 1, "snapshot_digest": strings.Repeat("e", 64), "items": []any{map[string]any{"binding": binding, "ordinal": 0, "delta": delta, "disposition": "apply", "reason": nil}}})
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": corrections, "learning_version_id": learningID, "overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": delta})
	frozen, err := BuildFrozenRenderRecipe(authored, ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Session: &ResolvedCorrection{Ref: corrections, Vector: delta}, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, LearningID: learningID, Learning: &delta, LearningModelRef: modelRef, LearningDescriptorRef: shard, LearningAnalysisID: uuid.New(), ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := put(json.RawMessage(frozen))
	resolver := ClosureResolver{Catalog: f, Objects: f}
	refs, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), []LayerRef{{Path: "root", Contract: "FrozenRenderRecipe", Ref: root}}, binding)
	if err != nil || len(refs) != 648 || len(refs)+f.reads != len(f.lookups) {
		t.Fatal(len(refs), len(f.lookups), err)
	}
	faces := 0
	for _, node := range f.lookups {
		if strings.Contains(node.Path, ".frames[") && node.Contract == "WorkingImage" {
			if node.ExpectedBinding == nil || node.ExpectedBinding.Source != node.Ref || node.ExpectedBinding.CanvasHash != node.CanvasHash {
				t.Fatal("descriptor frame source binding lost", node.Path)
			}
		}
		if strings.Contains(node.Path, ".frames[") && node.Contract == "NpyArray" && node.ExpectedBinding == nil {
			t.Fatal("descriptor pixel frame binding lost", node.Path)
		}
		if strings.Contains(node.Path, ".frames[") && node.RenderFrame == nil {
			t.Fatal("frame analysis authority lost", node.Path)
		}
		if node.RenderFrame != nil && (node.RenderFrame.Root != root || node.RenderFrame.Descriptor != shard || node.RenderFrame.SelectedVersion != binding.AssetVersionID || node.RenderFrame.ModelLockSHA != strings.Repeat("d", 64)) {
			t.Fatal("frame authority changed", node.Path)
		}
		if node.Contract == "FaceObservations" {
			faces++
		}
	}
	if faces != 256 {
		t.Fatal("face closure incomplete", faces)
	}
	f.lookups = nil
	evidence, err := resolver.ResolveTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	entries := evidence.Entries
	if err != nil {
		t.Fatal("full roots rejected by task resolver", err)
	}
	// Compare contextual parsing with actual scoped traversal for every typed
	// parent read, including all 128 descriptor frames and binary descendants.
	for _, document := range evidence.Documents {
		var parent *LayerRef
		for _, occurrence := range evidence.Occurrences {
			if occurrence.Node.Ref == document.Ref && occurrence.Node.Path == document.Path && occurrence.Node.Contract == document.Contract {
				owned := occurrence.Node
				parent = &owned
				break
			}
		}
		if parent == nil {
			t.Fatal("document lacks occurrence", document.Path)
		}
		children, parseErr := DocumentOccurrenceChildren([]byte(document.JCS), *parent)
		if parseErr != nil {
			if parent.Contract == "StyleV2" {
				continue
			}
			t.Fatal("context parser rejected resolved parent", parent.Path, parseErr)
		}
		for _, child := range children {
			found := false
			for _, occurrence := range evidence.Occurrences {
				n := occurrence.Node
				if n.Path == child.Path && n.Ref == child.Ref && n.ParentRef == child.ParentRef && n.Contract == child.Contract && reflect.DeepEqual(n.ExpectedBinding, child.ExpectedBinding) && reflect.DeepEqual(n.ExpectedLearning, child.ExpectedLearning) && n.ExpectedModelLockSHA == child.ExpectedModelLockSHA && reflect.DeepEqual(n.RenderFrame, child.RenderFrame) {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("derived child absent from scoped evidence", child.Path)
			}
		}
	}
	frameFaces := 0
	for _, occurrence := range evidence.Occurrences {
		if occurrence.Node.RenderFrame == nil {
			continue
		}
		frame := occurrence.Node.RenderFrame
		if frame.Root != root || frame.Descriptor != shard || frame.SelectedVersion != binding.AssetVersionID || frame.ModelLockSHA != strings.Repeat("d", 64) || occurrence.Node.ExpectedBinding == nil {
			t.Fatal("occurrence evidence lost frame/source authority", occurrence.Node.Path)
		}
		if occurrence.Node.Contract == "FaceObservations" {
			frameFaces++
		}
	}
	if frameFaces != 128 {
		t.Fatal("frame occurrences omitted", frameFaces)
	}
	// Repeated source descendants have their own scoped authorization. A root
	// pixel lookup cannot authorize the recipe source occurrence's pixel path.
	f.denyPath = "TaskInputs[0].recipe.source.source.pixels"
	deniedEvidence, deniedErr := resolver.ResolveTaskInputEvidence(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}, binding)
	f.denyPath = ""
	if !errors.Is(deniedErr, v2input.ErrInvalid) || deniedEvidence.Entries != nil || deniedEvidence.Documents != nil || deniedEvidence.Occurrences != nil {
		t.Fatal("repeated source descendant denial exposed evidence", deniedErr)
	}
	seen := map[uuid.UUID]v2input.Entry{}
	for _, entry := range entries {
		seen[entry.Ref.ArtifactID] = entry
	}
	if seen[root.ArtifactID].Contract != "FrozenRenderRecipe" || seen[source.ArtifactID].Contract != "WorkingImage" || seen[image.Pixels.ID].Contract != "NpyArray" {
		t.Fatal("full source/root/pixel inventory missing")
	}
	if distinct && len(f.lookups) <= 512 {
		t.Fatal("full occurrence traversal was truncated to legacy budget", len(f.lookups))
	}
	if distinct {
		counts := map[string]int{}
		for _, entry := range entries {
			counts[entry.Contract]++
		}
		if counts["WorkingImage"] != 128 || counts["NpyArray"] != 128 || counts["FaceObservations"] != 128 {
			t.Fatal("distinct descriptor inputs missing", counts)
		}
	}
	selected := FrozenRenderLearning{VersionID: learningID, Model: modelRef, Descriptor: shard, AnalysisID: uuid.New(), Ordinal: 0}
	missing := selected
	missing.Ordinal = 4999
	descriptorRoots := []LayerRef{
		{Path: "first", Contract: "DescriptorShard", Ref: shard, ParentRef: root, ExpectedBinding: &binding, ExpectedLearning: &selected, ExpectedModelLockSHA: strings.Repeat("d", 64)},
		{Path: "repeated", Contract: "DescriptorShard", Ref: shard, ParentRef: root, ExpectedBinding: &binding, ExpectedLearning: &missing, ExpectedModelLockSHA: strings.Repeat("d", 64)},
	}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), append([]LayerRef{{Path: "full", Contract: "FrozenRenderRecipe", Ref: root}}, descriptorRoots[:1]...), binding); err != nil {
		t.Fatal("valid first descriptor occurrence rejected", err)
	}
	if out, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), append([]LayerRef{{Path: "full", Contract: "FrozenRenderRecipe", Ref: root}}, descriptorRoots...), binding); err == nil || out != nil {
		t.Fatal("repeated descriptor skipped selected-frame validation", out, err)
	}
	frameDenied := ClosureResolver{Catalog: deniedFramePixelsCatalog{ArtifactCatalog: f}, Objects: f}
	if out, err := frameDenied.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}, binding); err == nil || out != nil {
		t.Fatal("shared pixels skipped authorization for final frame", out, err)
	}
	denied := &deniedRenderCatalog{ArtifactCatalog: f, blocked: shard.ArtifactID}
	deniedResolver := ClosureResolver{Catalog: denied, Objects: f}
	if out, err := deniedResolver.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), []v2input.Entry{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}, binding); err == nil || out != nil || denied.denials != 1 {
		t.Fatal("denied descendant produced task inputs", out, err)
	}
	collision := root
	collision.ArtifactID = source.ArtifactID
	before := len(f.lookups)
	for _, bad := range [][]v2input.Entry{
		{{Ref: root, Contract: "FrozenRenderRecipe"}},
		{{Ref: collision, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}},
		{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}, {Ref: style, Contract: "Params"}},
		{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: root, Contract: "FrozenRenderRecipe"}},
	} {
		if _, err := resolver.ResolveTaskInputs(context.Background(), auth.Identity{}, uuid.New(), bad, binding); err == nil {
			t.Fatal("mixed or incomplete full-render roots accepted")
		}
	}
	if len(f.lookups) != before {
		t.Fatal("invalid full roots performed catalog IO")
	}
	t.Logf("authorized occurrences: %d", len(refs))
	if distinct {
		f.lookups = nil
		readsBefore := f.reads
		roots := make([]LayerRef, maxRenderDocumentClosure+1)
		for i := range roots {
			roots[i] = LayerRef{Path: "root", Contract: "FrozenRenderRecipe", Ref: root}
		}
		if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, binding); err == nil {
			t.Fatal("full-render occurrence cap bypassed")
		}
		if len(f.lookups) != maxRenderDocumentClosure+f.reads-readsBefore {
			t.Fatal("cap must reject before the next catalog lookup", len(f.lookups))
		}
	}
}

type deniedRenderCatalog struct {
	ArtifactCatalog
	blocked uuid.UUID
	denials int
}

func (c *deniedRenderCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	if node.Ref.ArtifactID == c.blocked {
		c.denials++
		return objectstore.Object{}, errors.New("revoked descriptor authority")
	}
	return c.ArtifactCatalog.Lookup(ctx, who, tenant, node)
}

type deniedFramePixelsCatalog struct{ ArtifactCatalog }

func (s deniedFramePixelsCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	if node.Contract == "WorkingPixels" && node.RenderFrame != nil && node.RenderFrame.Ordinal == 127 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return s.ArtifactCatalog.Lookup(ctx, who, tenant, node)
}

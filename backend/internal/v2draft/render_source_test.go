package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestRenderRecipeSourceLoadsFullCompositionWithoutDoubleOverride(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("image fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	pin := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	sourceRef := pin(images[0])
	canvasHash, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: canvasHash}
	styleRaw, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	styleRaw, err = canonical.Bytes(styleRaw)
	if err != nil {
		t.Fatal(err)
	}
	styleRef := pin(styleRaw)
	plan, _ := canonical.Marshal(map[string]any{"schema_version": 1, "snapshot_digest": testDigest('a'), "items": []any{map[string]any{"binding": binding, "ordinal": 0, "delta": CorrectionVector{EV: .2}, "disposition": "apply", "reason": nil}}})
	planRef := pin(plan)
	// Complete global values are absolute overrides, not an additive EV=1.
	var style map[string]any
	if json.Unmarshal(styleRaw, &style) != nil {
		t.Fatal("style fixture")
	}
	global := style["global"].(map[string]any)
	global["gains_rgb"] = []float64{2, 2, 2}
	override, _ := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256, "modules": map[string]any{"global": global, "extensions": map[string]any{"background": map[string]any{"mode": "blur", "asset_id": nil, "blur_strength": .4, "protect_subject": true}}}, "revision": 1})
	overrideRef := pin(override)
	layers, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	layerRef := pin(layers)
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": styleRef, "session_corrections": planRef, "learning_version_id": nil, "overrides": []v2input.Ref{overrideRef}, "layer_stack": layerRef, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{EV: .1}})
	store := closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	put := func(ref v2input.Ref, key string, raw []byte, media string) {
		store.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256, Bytes: int64(len(raw)), VersionID: "v1", ContentType: media}
		store.bodies[key] = raw
	}
	put(sourceRef, "source", images[0], "application/json")
	put(styleRef, "style", styleRaw, "application/json")
	put(planRef, "plan", plan, "application/json")
	put(overrideRef, "override", override, "application/json")
	put(layerRef, "layers", layers, "application/json")
	put(v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, "pixels", []byte("fixture"), "application/octet-stream")
	source := RenderRecipeSource{Resolver: ClosureResolver{Catalog: store, Objects: store}, ProjectID: uuid.New()}
	tenant, document := uuid.New(), uuid.New()
	modelLock := testDigest('e')
	loaded, err := source.Load(context.Background(), auth.Identity{}, tenant, document, authored, binding, image.Color.ICCSHA256, modelLock)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.LayerBody) != string(layers) || string(loaded.StyleBody) != string(styleRaw) || len(loaded.OverrideBodies) != 1 || string(loaded.OverrideBodies[0]) != string(override) || loaded.SourceSHA != image.Canvas.SourceSHA256 || loaded.Inputs.Overrides[0].Vector != (CorrectionVector{}) {
		t.Fatal("composition lost or override double-counted")
	}
	frozen, err := BuildFrozenRenderRecipe(loaded.Authored, loaded.Inputs, loaded.LayerBody)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeFrozenRenderRecipe(frozen)
	if err != nil || result.Scalars.EffectiveDelta.EV < .299999 || result.Scalars.EffectiveDelta.EV > .300001 {
		t.Fatal("wrong scalar composition", err)
	}
	authored[0] = '!'
	if loaded.Authored[0] == '!' {
		t.Fatal("borrowed authored bytes")
	}
	valid := loaded.Authored
	if _, err = source.Load(context.Background(), auth.Identity{}, tenant, document, valid, binding, testDigest('f'), modelLock); err == nil {
		t.Fatal("foreign ICC accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = source.Load(canceled, auth.Identity{}, tenant, document, valid, binding, image.Color.ICCSHA256, modelLock); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}

	var learnedFields map[string]any
	if json.Unmarshal(valid, &learnedFields) != nil {
		t.Fatal("authored fixture")
	}
	version := uuid.New()
	learnedFields["learning_version_id"] = version
	learnedRaw, _ := canonical.Marshal(learnedFields)
	if _, err = source.Load(context.Background(), auth.Identity{}, tenant, document, learnedRaw, binding, image.Color.ICCSHA256, modelLock); err == nil {
		t.Fatal("missing learning resolver accepted")
	}
	profile := CandidateProfile{SchemaVersion: 1, ProfileID: uuid.New(), TrainingSHA: testDigest('a'), FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	profile.Intercept[0] = .1
	learning := &renderLearningFixture{model: EligibleLearningModel{Profile: profile, ModelRef: pin([]byte("model")), ManifestRef: pin([]byte("manifest")), ICCLockSHA: image.Color.ICCSHA256, ModelLockSHA: modelLock}, frame: PinnedFrameDescriptor{AnalysisID: uuid.New(), ShardRef: pin([]byte("descriptor")), Features: FrameFeatures{Binding: binding, Faces: pin([]byte("faces")), LumaQuantiles: [5]float64{.1, .2, .5, .7, .9}, Sharpness: 1, PersonFraction: .5}}}
	source.Learning = learning
	learned, err := source.Load(context.Background(), auth.Identity{}, tenant, document, learnedRaw, binding, image.Color.ICCSHA256, modelLock)
	if err != nil || learned.Inputs.LearningID != version || learned.Inputs.Learning == nil || learned.Inputs.Learning.EV != .1 || learned.Inputs.Session.Vector.EV != .2 || learned.Inputs.LearningDescriptorRef != learning.frame.ShardRef {
		t.Fatal("learning pins/prediction or callback ownership", err)
	}
	learning.model.ModelLockSHA = testDigest('f')
	learning.frameCalls = 0
	if _, err = source.Load(context.Background(), auth.Identity{}, tenant, document, learnedRaw, binding, image.Color.ICCSHA256, modelLock); err == nil || learning.frameCalls != 0 {
		t.Fatal("foreign learning lock reached descriptor")
	}
	learning.model.ModelLockSHA = modelLock
	learning.frame.Features.Binding.AssetVersionID = uuid.New()
	if _, err = source.Load(context.Background(), auth.Identity{}, tenant, document, learnedRaw, binding, image.Color.ICCSHA256, modelLock); err == nil {
		t.Fatal("foreign descriptor binding accepted")
	}
	store.bodies["override"] = []byte(`{}`)
	if _, err = source.Load(context.Background(), auth.Identity{}, tenant, document, valid, binding, image.Color.ICCSHA256, modelLock); err == nil {
		t.Fatal("changed object bytes accepted")
	}
}

type renderLearningFixture struct {
	model      EligibleLearningModel
	frame      PinnedFrameDescriptor
	frameCalls int
}

func (f *renderLearningFixture) ReadEligibleLearningProfile(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (EligibleLearningModel, error) {
	return f.model, nil
}
func (f *renderLearningFixture) ReadFrameDescriptor(_ context.Context, _ auth.Identity, _ uuid.UUID, _ uuid.UUID, _ v2input.SessionVersionBinding, _ string, session *ResolvedCorrection) (PinnedFrameDescriptor, error) {
	f.frameCalls++
	if session != nil {
		session.Vector.EV = .9
	}
	return f.frame, nil
}

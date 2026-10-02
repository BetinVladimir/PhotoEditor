package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type trainingRecipeStore struct {
	objects map[uuid.UUID]objectstore.Object
	bodies  map[string][]byte
}

type trainingLearningReader struct {
	model      v2draft.EligibleLearningModel
	frame      v2draft.PinnedFrameDescriptor
	documentID uuid.UUID
}

func (r *trainingLearningReader) ReadEligibleLearningProfile(_ context.Context,
	_ auth.Identity, _, _, documentID, _ uuid.UUID) (v2draft.EligibleLearningModel, error) {
	r.documentID = documentID
	return r.model, nil
}

func (r *trainingLearningReader) ReadFrameDescriptor(_ context.Context,
	_ auth.Identity, _, _ uuid.UUID, _ v2input.SessionVersionBinding,
	_ string, _ *v2draft.ResolvedCorrection) (v2draft.PinnedFrameDescriptor, error) {
	return r.frame, nil
}

func (s trainingRecipeStore) Lookup(_ context.Context, _ auth.Identity,
	_ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	obj, okay := s.objects[node.Ref.ArtifactID]
	if !okay || obj.SHA256 != node.Ref.SHA256 {
		return objectstore.Object{}, ErrInvalidTrainingDraft
	}
	return obj, nil
}

func (s trainingRecipeStore) Open(_ context.Context,
	obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.bodies[obj.Key])), nil
}

func TestTrainingRecipeSourceResolvesScopedScalarPair(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil || len(images) == 0 {
		t.Fatal("missing working image fixture")
	}
	imageBody := []byte(images[0])
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	styleBody, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	styleBody, err = canonical.Bytes(styleBody)
	if err != nil {
		t.Fatal(err)
	}
	makeRef := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: hex.EncodeToString(sum[:])}
	}
	imageRef, styleRef := makeRef(imageBody), makeRef(styleBody)
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: imageRef, CanvasHash: canvasHash}
	recipe := func(ev float64, learning any) []byte {
		body, err := canonical.Marshal(map[string]any{
			"schema_version": 3, "source": binding, "base_style": styleRef,
			"session_corrections": nil, "learning_version_id": learning,
			"overrides": []any{}, "layer_stack": nil,
			"renderer_version": "v2.2",
			"parameter_delta": map[string]any{"ev": ev,
				"log_r": 0, "log_g": 0, "log_b": 0,
				"saturation_delta": 0, "retouch_delta": 0},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	pixel := v2input.Ref{ArtifactID: image.Pixels.ID,
		SHA256: image.Pixels.SHA256}
	store := trainingRecipeStore{objects: map[uuid.UUID]objectstore.Object{},
		bodies: map[string][]byte{"image": imageBody, "style": styleBody}}
	store.objects[imageRef.ArtifactID] = objectstore.Object{Key: "image",
		SHA256: imageRef.SHA256, Bytes: int64(len(imageBody)),
		VersionID: "v1", ContentType: "application/json"}
	store.objects[styleRef.ArtifactID] = objectstore.Object{Key: "style",
		SHA256: styleRef.SHA256, Bytes: int64(len(styleBody)),
		VersionID: "v1", ContentType: "application/json"}
	store.objects[pixel.ArtifactID] = objectstore.Object{Key: "pixels",
		SHA256: pixel.SHA256, Bytes: 10, VersionID: "v1",
		ContentType: "application/octet-stream"}
	documents := TrainingEditingDocuments{BaseBody: recipe(0, nil),
		ApprovedBody: recipe(.1, nil), Binding: binding, BaseStyle: styleRef}
	source := TrainingRecipeSource{Catalog: store, Objects: store}
	got, session, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil || session != nil || got.Approved.EffectiveDelta.EV != .1 ||
		got.Base.EffectiveDelta.EV != 0 {
		t.Fatalf("scalar pair not resolved: %v", err)
	}
	overrideBody, err := v2draft.BuildAnalysisOverride(styleBody,
		image.Canvas.SourceSHA256, v2draft.CorrectionVector{
			EV: .2, RetouchDelta: .1})
	if err != nil {
		t.Fatal(err)
	}
	overrideRef := makeRef(overrideBody)
	store.objects[overrideRef.ArtifactID] = objectstore.Object{Key: "override",
		SHA256: overrideRef.SHA256, Bytes: int64(len(overrideBody)),
		VersionID: "v1", ContentType: "application/json"}
	store.bodies["override"] = overrideBody
	var overrideEnvelope struct {
		Modules map[string]json.RawMessage `json:"modules"`
	}
	json.Unmarshal(overrideBody, &overrideEnvelope)
	if _, err := scalarBeautySmoothing(overrideEnvelope.Modules["beauty"],
		map[string]json.RawMessage{"enabled": json.RawMessage("false")}, false); err != nil {
		t.Fatalf("beauty scalar: %v", err)
	}
	var approvedMap map[string]any
	if json.Unmarshal(documents.ApprovedBody, &approvedMap) != nil {
		t.Fatal("invalid recipe fixture")
	}
	approvedMap["overrides"] = []v2input.Ref{overrideRef}
	documents.ApprovedBody, err = canonical.Marshal(approvedMap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.loadScalarOverrides(context.Background(), auth.Identity{},
		uuid.New(), []v2input.Ref{overrideRef}, styleRef,
		v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1},
		nil, nil); err != nil {
		t.Fatalf("scalar override source: %v", err)
	}
	got, _, err = source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil || math.Abs(got.Approved.EffectiveDelta.EV-.3) > 1e-10 ||
		math.Abs(got.Approved.EffectiveDelta.RetouchDelta-.1) > 1e-10 {
		t.Fatalf("ordered scalar override not resolved: %v", err)
	}
	var altered map[string]json.RawMessage
	var modules map[string]json.RawMessage
	var global map[string]json.RawMessage
	if json.Unmarshal(overrideBody, &altered) != nil ||
		json.Unmarshal(altered["modules"], &modules) != nil ||
		json.Unmarshal(modules["global"], &global) != nil {
		t.Fatal("invalid override fixture")
	}
	global["master_curve"] = json.RawMessage(`[0,0,0,0,0,0,0]`)
	modules["global"], _ = canonical.Marshal(global)
	altered["modules"], _ = canonical.Marshal(modules)
	changedBody, _ := canonical.Marshal(altered)
	changedRef := makeRef(changedBody)
	store.objects[changedRef.ArtifactID] = objectstore.Object{Key: "changed",
		SHA256: changedRef.SHA256, Bytes: int64(len(changedBody)),
		VersionID: "v1", ContentType: "application/json"}
	store.bodies["changed"] = changedBody
	approvedMap["overrides"] = []v2input.Ref{changedRef}
	documents.ApprovedBody, _ = canonical.Marshal(approvedMap)
	if _, _, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("non-scalar global change accepted for parameter training")
	}
	approvedMap["overrides"] = []v2input.Ref{overrideRef}
	documents.ApprovedBody, _ = canonical.Marshal(approvedMap)
	delete(store.objects, pixel.ArtifactID)
	if _, _, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("missing nested pixel accepted")
	}
	store.objects[pixel.ArtifactID] = objectstore.Object{Key: "pixels",
		SHA256: pixel.SHA256, Bytes: 10, VersionID: "v1",
		ContentType: "application/octet-stream"}
	documents.ApprovedBody = recipe(.1, uuid.New())
	if _, _, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("unresolved learning dependency accepted")
	}
	modelRef, descriptorRef := makeRef([]byte("model")), makeRef([]byte("descriptor"))
	manifestRef, facesRef := makeRef([]byte("manifest")), makeRef([]byte("faces"))
	profile := v2draft.CandidateProfile{SchemaVersion: 1, ProfileID: uuid.New(),
		TrainingSHA: strings.Repeat("c", 64), FeatureOrder: "scene12_v1", RidgeLambda: .1,
		Intercept: [6]float64{.2}}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	learning := &trainingLearningReader{model: v2draft.EligibleLearningModel{
		Profile: profile, ModelRef: modelRef, ManifestRef: manifestRef,
		ICCLockSHA:   strings.Repeat("a", 64),
		ModelLockSHA: strings.Repeat("b", 64)}, frame: v2draft.PinnedFrameDescriptor{
		AnalysisID: uuid.New(), ShardRef: descriptorRef, Ordinal: 2,
		Features: v2draft.FrameFeatures{Binding: binding, Faces: facesRef}}}
	documents.ApprovedDocumentID = uuid.New()
	source.Learning, source.ProjectID = learning, uuid.New()
	got, _, err = source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil || learning.documentID != documents.ApprovedDocumentID ||
		math.Abs(got.Approved.EffectiveDelta.EV-.3) > 1e-9 {
		t.Fatalf("learning pair not resolved against approved document: %v", err)
	}
	learning.frame.Features.Binding.AssetVersionID = uuid.New()
	if _, _, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("descriptor for another asset version accepted")
	}
	learning.frame.Features.Binding = binding
	learning.model.ModelLockSHA = strings.Repeat("d", 64)
	if _, _, err := source.LoadPair(context.Background(), auth.Identity{},
		uuid.New(), documents, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("mismatched model lock accepted")
	}
}

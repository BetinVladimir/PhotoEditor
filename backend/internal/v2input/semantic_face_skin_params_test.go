package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestSemanticSkinPreservesParserMapAndRejectsSubstitution(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile("../review/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var images []json.RawMessage
	if err := json.Unmarshal(read("working-images-python.json"), &images); err != nil {
		t.Fatal(err)
	}
	pin := func(raw []byte) Ref {
		sum := sha256.Sum256(raw)
		return Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	marshal := func(value any) []byte {
		t.Helper()
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, imageBody := range images {
		image, err := review.ParseWorkingImage(imageBody)
		if err != nil {
			t.Fatal(err)
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil {
			t.Fatal(err)
		}
		binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin(imageBody), CanvasHash: canvas}
		source := review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}
		var boxes review.FaceBoxes
		if err := json.Unmarshal(read("face-boxes-python.json"), &boxes); err != nil {
			t.Fatal(err)
		}
		boxes.Image = source
		detectionBody := marshal(boxes)
		detection := pin(detectionBody)
		var labels review.FaceLabels19
		if err := json.Unmarshal(read("face-labels19-python.json"), &labels); err != nil {
			t.Fatal(err)
		}
		labels.Image, labels.Faces = source, review.ArtifactRef{ID: detection.ArtifactID, SHA256: detection.SHA256}
		labels.Canvas, labels.TransformChain = image.Canvas, image.TransformChain
		labels.ParsedFaces = len(boxes.Faces)
		labelsBody := marshal(labels)
		labelsRef := pin(labelsBody)
		parserBody := marshal(review.FaceParseParams{Image: source, Faces: labels.Faces, ModelID: labels.ModelID, ModelLockID: labels.ModelLockID, PreprocessingID: labels.PreprocessingID, CropSize: 512, Expansion: 1.35})
		parserRef := pin(parserBody)
		got, err := PrepareSemanticFaceSkinParams(context.Background(), binding, imageBody, labelsRef, labelsBody, parserRef, parserBody, detection, detectionBody)
		if err != nil {
			t.Fatal(err)
		}
		want := map[uuid.UUID]Entry{labelsRef.ArtifactID: {Ref: labelsRef, Contract: "FaceLabels19"}, labels.Labels.ID: {Ref: Ref{ArtifactID: labels.Labels.ID, SHA256: labels.Labels.SHA256}, Contract: "NpyArray"}}
		workflow, ownParams := uuid.New(), pin(got.Params)
		sizes := []SemanticInputSize{{Ref: ownParams, Bytes: int64(len(got.Params))}}
		for _, entry := range got.Inputs {
			sizes = append(sizes, SemanticInputSize{Ref: entry.Ref, Bytes: 512})
		}
		frozen, err := CompileSemanticFaceSkinTask(context.Background(), workflow, 0, binding, imageBody, labelsRef, labelsBody, parserRef, parserBody, detection, detectionBody, ownParams, sizes, 1<<40, 1<<40, 1<<40)
		if err != nil {
			t.Fatal(err)
		}
		indexed, err := frozen.WithInputIndexScratch()
		if err != nil {
			t.Fatal("indexed skin", err)
		}
		assertSemanticSkinIndexBudget(t, frozen, indexed)
		if _, err := indexed.WithInputIndexScratch(); err == nil {
			t.Fatal("doubled skin index budget")
		}
		assertSemanticInventoryBoundary(t, frozen.VerifyInputInventory, got.Inputs, ownParams)
		draft, err := frozen.DraftTask()
		if err != nil || draft.Operation != "mask.face_skin" || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != ownParams.ArtifactID {
			t.Fatal("skin task routing", err)
		}
		draft.ExecutionProfile[0] = ' '
		again, err := frozen.DraftTask()
		if err != nil || again.ExecutionProfile[0] != '{' {
			t.Fatal("mutable frozen profile", err)
		}
		published, err := PublishSemanticFaceSkinTask(context.Background(), frozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
		if err != nil || published.Graph.Built.Count != 3 {
			t.Fatal("skin publication", err)
		}
		changed := published.Graph.Built
		changed.Count--
		if _, err := frozen.BindPublishedTransport(context.Background(), changed); err == nil {
			t.Fatal("truncated skin transport accepted")
		}
		if _, err := CompileSemanticFaceSkinTask(context.Background(), workflow, 0, binding, imageBody, labelsRef, labelsBody, parserRef, parserBody, detection, detectionBody, ownParams, sizes[:2], 1<<40, 1<<40, 1<<40); err == nil {
			t.Fatal("missing catalog size accepted")
		}
		closure, err := CompileSemanticFaceSkinClosure(context.Background(), workflow, 0, binding, imageBody, labelsRef, labelsBody, parserRef, parserBody, detection, detectionBody, ownParams)
		if err != nil {
			t.Fatal(err)
		}
		cursor, err := closure.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		seen := map[uuid.UUID]Entry{}
		for {
			entry, ok, err := cursor.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			seen[entry.Ref.ArtifactID] = entry
		}
		if len(seen) != 3 || seen[ownParams.ArtifactID] != (Entry{Ref: ownParams, Contract: "Params"}) {
			t.Fatal("skin transport contains wrong inputs")
		}
		for id, entry := range want {
			if seen[id] != entry {
				t.Fatal("skin map lost from transport")
			}
		}
		if len(closure.Direct) != 3 || closure.Direct[0] != ownParams || closure.Direct[1] != labelsRef || closure.Direct[2].ArtifactID != labels.Labels.ID {
			t.Fatal("skin bootstrap mismatch")
		}
		for _, bad := range []Ref{parserRef, detection, labelsRef, binding.Source, {ArtifactID: labels.Labels.ID, SHA256: labels.Labels.SHA256}, {ArtifactID: ownParams.ArtifactID, SHA256: binding.Source.SHA256}} {
			if _, err := CompileSemanticFaceSkinClosure(context.Background(), workflow, 0, binding, imageBody, labelsRef, labelsBody, parserRef, parserBody, detection, detectionBody, bad); err == nil {
				t.Fatal("skin Params substitution accepted")
			}
		}
		if len(got.Inputs) != 2 {
			t.Fatal("incorrect skin inputs")
		}
		for _, entry := range got.Inputs {
			if want[entry.Ref.ArtifactID] != entry {
				t.Fatal("parser map replaced")
			}
		}
		reject := func(body []byte, ref Ref) {
			t.Helper()
			result, err := PrepareSemanticFaceSkinParams(context.Background(), binding, imageBody, ref, body, parserRef, parserBody, detection, detectionBody)
			if err == nil || !reflect.DeepEqual(result, SemanticFaceSkinPreparation{}) {
				t.Fatal("substituted parser manifest accepted")
			}
		}
		reject(append(append([]byte{}, labelsBody...), ' '), labelsRef)
		labels.Labels.ID = binding.Source.ArtifactID
		aliased := marshal(labels)
		reject(aliased, pin(aliased))
		labels.Labels.ID = uuid.New()
		labels.Image.ID = uuid.New()
		foreign := marshal(labels)
		reject(foreign, pin(foreign))
	}
}

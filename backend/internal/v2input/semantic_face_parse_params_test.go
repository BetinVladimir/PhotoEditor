package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestSemanticFaceParseExactDetectorBinding(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../review/testdata/face-boxes-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture review.FaceBoxes
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	refFor := func(body []byte) Ref {
		sum := sha256.Sum256(body)
		return Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
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
		binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: refFor(imageBody), CanvasHash: canvas}
		boxes := fixture
		boxes.Image = review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}
		facesBody, err := canonical.Marshal(boxes)
		if err != nil {
			t.Fatal(err)
		}
		detector, err := PrepareSemanticFaceDetectParams(context.Background(), binding, imageBody, boxes.ModelLockID, .8, 64)
		if err != nil {
			t.Fatal(err)
		}
		faces, params := refFor(facesBody), refFor(detector.Params)
		got, err := PrepareSemanticFaceParseParams(context.Background(), binding, imageBody, faces, facesBody, params, detector.Params, strings.Repeat("b", 64))
		if err != nil {
			t.Fatal(err)
		}
		var compiled review.FaceParseParams
		if err := json.Unmarshal(got.Params, &compiled); err != nil {
			t.Fatal(err)
		}
		if compiled.Image != boxes.Image || compiled.Faces != (review.ArtifactRef{ID: faces.ArtifactID, SHA256: faces.SHA256}) || compiled.ModelID != "bisenet_face19" || compiled.PreprocessingID != "bisenet_face19_crop512_v1" || compiled.CropSize != 512 || compiled.Expansion != 1.35 {
			t.Fatal("parse worker contract mismatch")
		}
		expected := append(append([]Entry{}, detector.Inputs...), Entry{Ref: faces, Contract: "FaceBoxes"})
		workflow, parseParams := uuid.New(), refFor(got.Params)
		sizes := []SemanticInputSize{{Ref: parseParams, Bytes: int64(len(got.Params))}}
		for _, entry := range got.Inputs {
			sizes = append(sizes, SemanticInputSize{Ref: entry.Ref, Bytes: 512})
		}
		frozen, err := CompileSemanticFaceParseTask(context.Background(), workflow, 0, binding, imageBody, faces, facesBody, params, detector.Params, parseParams, strings.Repeat("b", 64), sizes, SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		assertSemanticInventoryBoundary(t, frozen.VerifyInputInventory, got.Inputs, parseParams)
		identified, identityErr := frozen.WithExecutionIdentity(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64))
		if identityErr != nil {
			t.Fatal("identified native face task", identityErr)
		}
		assertSemanticModelIdentity(t, frozen, identified, strings.Repeat("b", 64))
		if _, identityErr := identified.WithExecutionIdentity(strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)); identityErr == nil {
			t.Fatal("rebound native identity")
		}
		if _, identityErr := frozen.WithExecutionIdentity(strings.Repeat("f", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)); identityErr == nil {
			t.Fatal("foreign model identity")
		}
		draft, err := frozen.DraftTask()
		if err != nil || draft.Operation != "face.parse" || draft.ResourceClass != "gpu" || draft.ParamsArtifactID != parseParams.ArtifactID {
			t.Fatal("BiSeNet task routing", err)
		}
		draft.ExecutionProfile[0] = ' '
		again, err := frozen.DraftTask()
		if err != nil || again.ExecutionProfile[0] != '{' {
			t.Fatal("BiSeNet profile mutated", err)
		}
		published, err := PublishSemanticFaceParseTask(context.Background(), frozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
		if err != nil || published.Graph.Built.Count != len(got.Inputs)+1 {
			t.Fatal("BiSeNet transport", err)
		}
		changed := published.Graph.Built
		changed.Count--
		if _, err := frozen.BindPublishedTransport(context.Background(), changed); err == nil {
			t.Fatal("truncated BiSeNet transport accepted")
		}
		if _, err := CompileSemanticFaceParseTask(context.Background(), workflow, 0, binding, imageBody, faces, facesBody, params, detector.Params, parseParams, strings.Repeat("b", 64), sizes[:len(sizes)-1], SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30); err == nil {
			t.Fatal("missing BiSeNet input size accepted")
		}
		closure, err := CompileSemanticFaceParseClosure(context.Background(), workflow, 0, binding, imageBody, faces, facesBody, params, detector.Params, parseParams, strings.Repeat("b", 64))
		if err != nil {
			t.Fatal("parser closure", err)
		}
		cursor, err := closure.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		count, foundFaces, foundParams := 0, false, false
		for {
			entry, ok, err := cursor.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			count++
			foundFaces = foundFaces || entry == (Entry{Ref: faces, Contract: "FaceBoxes"})
			foundParams = foundParams || entry == (Entry{Ref: parseParams, Contract: "Params"})
			if entry.Ref.ArtifactID == params.ArtifactID {
				t.Fatal("original detector Params replaced BiSeNet Params")
			}
		}
		if count != len(expected)+1 || !foundFaces || !foundParams || len(closure.Direct) != 3 || closure.Direct[0] != parseParams || closure.Direct[1] != binding.Source {
			t.Fatal("incomplete parser transport")
		}
		for _, bad := range []Ref{params, faces, binding.Source, {ArtifactID: parseParams.ArtifactID, SHA256: strings.Repeat("f", 64)}} {
			if _, err := CompileSemanticFaceParseClosure(context.Background(), workflow, 0, binding, imageBody, faces, facesBody, params, detector.Params, bad, strings.Repeat("b", 64)); err == nil {
				t.Fatal("aliased or modified parser Params accepted")
			}
		}
		if len(got.Inputs) != len(expected) {
			t.Fatal("source closure lost")
		}
		for _, want := range expected {
			found := false
			for _, actual := range got.Inputs {
				if actual == want {
					found = true
				}
			}
			if !found {
				t.Fatal("source or detector output replaced")
			}
		}
		deny := func(name string, faceRef Ref, body []byte, paramRef Ref, paramBody []byte) {
			t.Helper()
			result, err := PrepareSemanticFaceParseParams(context.Background(), binding, imageBody, faceRef, body, paramRef, paramBody, strings.Repeat("b", 64))
			if err == nil || !reflect.DeepEqual(result, SemanticFaceParsePreparation{}) {
				t.Fatalf("%s accepted", name)
			}
		}
		deny("changed output bytes", faces, append(append([]byte{}, facesBody...), ' '), params, detector.Params)
		deny("changed original Params bytes", faces, facesBody, params, append(append([]byte{}, detector.Params...), ' '))
		foreign := boxes
		foreign.Image.ID = uuid.New()
		foreignBody, err := canonical.Marshal(foreign)
		if err != nil {
			t.Fatal(err)
		}
		deny("foreign source with correct output digest", refFor(foreignBody), foreignBody, params, detector.Params)
		alias := faces
		alias.ArtifactID = binding.Source.ArtifactID
		deny("source alias", alias, facesBody, params, detector.Params)
		var wrong review.FaceDetectParams
		if err := json.Unmarshal(detector.Params, &wrong); err != nil {
			t.Fatal(err)
		}
		wrong.ModelLockID = strings.Repeat("d", 64)
		wrongBody, err := canonical.Marshal(wrong)
		if err != nil {
			t.Fatal(err)
		}
		deny("detector model mismatch with correct Params digest", faces, facesBody, refFor(wrongBody), wrongBody)
	}
}

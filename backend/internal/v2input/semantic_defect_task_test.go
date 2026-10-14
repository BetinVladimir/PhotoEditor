package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestFrozenDefectTaskKeepsExactNativeInputs(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(images[0])
	canvas, _ := canonical.Hash(image.Canvas)
	binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: Ref{uuid.New(), hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}, images[0], []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	params := Ref{uuid.New(), compiled.ParamsSHA256}
	pixels := Ref{image.Pixels.ID, image.Pixels.SHA256}
	entries := []Entry{{params, "Params"}, {binding.Source, "WorkingImage"}, {pixels, "NpyArray"}}
	sizes := []SemanticInputSize{{params, int64(len(compiled.Params))}, {binding.Source, int64(len(images[0]))}, {pixels, 12*image.Canvas.Width*image.Canvas.Height + 128}}
	workflow := uuid.New()
	compile := func(in []Entry) (FrozenSemanticDefectTask, error) {
		return CompileSemanticDefectTask(context.Background(), workflow, 0, binding, images[0], compiled.Params, params, in, sizes, SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
	}
	frozen, err := compile(entries)
	if err != nil {
		t.Fatal(err)
	}
	again, err := compile(entries)
	if err != nil || again.task != frozen.task || again.closure.InputDigest != frozen.closure.InputDigest {
		t.Fatal("defect identity unstable", err)
	}
	draft, err := frozen.DraftTask()
	if err != nil || draft.Operation != "defects.propose" || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != params.ArtifactID {
		t.Fatal(draft, err)
	}
	transport, err := PublishSemanticDefectTask(context.Background(), frozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
	if err != nil || transport.Graph.Built.Count != 3 || transport.InputDigest != draft.InputDigest || transport.Params != params {
		t.Fatal("native defect transport", err)
	}
	changed := transport.Graph.Built
	changed.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := frozen.BindPublishedTransport(context.Background(), changed); err == nil {
		t.Fatal("changed defect transport accepted")
	}
	// A full 64-face inventory exceeds the direct-reference transport limit.
	faces := make([]review.DefectProposalFaceInput, 64)
	for i := range faces {
		faces[i] = review.DefectProposalFaceInput{
			Landmarks: review.ArtifactRef{ID: uuid.New(), SHA256: pixels.SHA256},
			Skin:      &review.ProtectionMask{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: pixels.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "skin", Confidence: 1},
		}
	}
	large, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}, images[0], faces, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	largeParams := Ref{uuid.New(), large.ParamsSHA256}
	largeEntries := []Entry{{largeParams, "Params"}}
	largeSizes := []SemanticInputSize{{largeParams, int64(len(large.Params))}}
	landmarks := map[uuid.UUID]bool{}
	for _, face := range faces {
		landmarks[face.Landmarks.ID] = true
	}
	for _, input := range large.Inputs {
		ref := Ref{input.ID, input.SHA256}
		contract := "NpyArray"
		if ref == binding.Source {
			contract = "WorkingImage"
		}
		if landmarks[input.ID] {
			contract = "FaceLandmarks"
		}
		largeEntries = append(largeEntries, Entry{ref, contract})
		largeSizes = append(largeSizes, SemanticInputSize{ref, 12*image.Canvas.Width*image.Canvas.Height + 1048576})
	}
	largeFrozen, err := CompileSemanticDefectTask(context.Background(), workflow, 0, binding, images[0], large.Params, largeParams, largeEntries, largeSizes, SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	largeTransport, err := PublishSemanticDefectTask(context.Background(), largeFrozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
	if err != nil || largeTransport.Graph.Built.Count != 131 || len(largeTransport.Graph.Direct) != 3 {
		t.Fatal("full face inventory lost or transport rejected", largeTransport.Graph.Built.Count, err)
	}
	draft.ExecutionProfile[0] = ' '
	fresh, err := frozen.DraftTask()
	if err != nil || fresh.ExecutionProfile[0] != '{' {
		t.Fatal("profile alias escaped", err)
	}
	entries[0].Contract = "changed"
	compiled.Params[0] = ' '
	cursor, err := frozen.closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if entry.Contract == "changed" {
			t.Fatal("inventory alias escaped")
		}
		count++
	}
	if count != 3 || frozen.closure.ParamsJCS[0] != '{' {
		t.Fatal("native payload changed")
	}
	if _, err := compile(entries); err == nil {
		t.Fatal("changed Params accepted")
	}
}

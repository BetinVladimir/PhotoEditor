package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestSemanticDetectorParamsMatchPythonWorker(t *testing.T) {
	raw, err := os.ReadFile("testdata/semantic-face-detect-params-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Binding SessionVersionBinding `json:"binding"`
		Index   int                   `json:"source_index"`
		Score   float64               `json:"score_threshold"`
		Maximum int                   `json:"max_faces"`
		Params  json.RawMessage       `json:"params"`
	}
	if json.Unmarshal(raw, &vectors) != nil || len(vectors) != 3 {
		t.Fatal("Python worker vectors")
	}
	raw, err = os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("source fixtures")
	}
	for _, vector := range vectors {
		if vector.Index < 0 || vector.Index >= len(images) {
			t.Fatal("source index")
		}
		got, err := PrepareSemanticFaceDetectParams(context.Background(), vector.Binding, images[vector.Index], strings.Repeat("a", 64), vector.Score, vector.Maximum)
		if err != nil {
			t.Fatal("Python-bound source preparation", err)
		}
		want, err := canonical.Bytes(vector.Params)
		if err != nil || !bytes.Equal(got.Params, want) {
			t.Fatal("Go/Python SFD Params mismatch", err)
		}
	}
}

func TestSemanticDetectorPreparationKeepsExactSourceChildren(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("source fixtures")
	}
	for _, body := range images {
		image, err := review.ParseWorkingImage(body)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		canvas, _ := canonical.Hash(image.Canvas)
		binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
		got, err := PrepareSemanticFaceDetectParams(context.Background(), binding, body, strings.Repeat("a", 64), .8, 64)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := ResolveSemanticSourceMetadata(binding, body)
		if err != nil {
			t.Fatal(err)
		}
		wanted := map[uuid.UUID]Ref{binding.Source.ArtifactID: binding.Source, metadata.Inputs.Pixels.ArtifactID: metadata.Inputs.Pixels}
		if metadata.Inputs.Alpha != nil {
			wanted[metadata.Inputs.Alpha.ArtifactID] = *metadata.Inputs.Alpha
		}
		for _, ref := range metadata.Geometry {
			wanted[ref.ArtifactID] = ref
		}
		if len(got.Inputs) != len(wanted) {
			t.Fatal("source child lost")
		}
		for _, entry := range got.Inputs {
			if wanted[entry.Ref.ArtifactID] != entry.Ref {
				t.Fatal("source child replaced")
			}
			if entry.Ref == binding.Source {
				if entry.Contract != "WorkingImage" {
					t.Fatal("image contract")
				}
			} else if entry.Contract != "NpyArray" {
				t.Fatal("array contract")
			}
		}
		var params review.FaceDetectParams
		if json.Unmarshal(got.Params, &params) != nil || params.Image != (review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}) || params.ModelID != "sfd" || params.PreprocessingID != "sfd_long1024_bgr_v1" || params.LongSide != 1024 {
			t.Fatal("SFD source/preprocessing changed")
		}
		paramsSum := sha256.Sum256(got.Params)
		paramsRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(paramsSum[:])}
		workflow := uuid.New()
		closure, err := CompileSemanticDetectorClosure(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), .8, 64)
		if err != nil || closure.Open == nil || len(closure.Direct) != 3 || !bytes.Equal(closure.ParamsJCS, got.Params) {
			t.Fatal("detector closure", err)
		}
		sizes := []SemanticInputSize{{Ref: paramsRef, Bytes: int64(len(got.Params))}}
		for _, entry := range got.Inputs {
			sizes = append(sizes, SemanticInputSize{Ref: entry.Ref, Bytes: 1024})
		}
		frozen, err := CompileSemanticDetectorTask(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), .8, 64, sizes, SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<20)
		if err != nil {
			t.Fatal("native detector task", err)
		}
		identified, identityErr := frozen.WithExecutionIdentity(strings.Repeat("a", 64), strings.Repeat("c", 64), strings.Repeat("d", 64))
		if identityErr != nil {
			t.Fatal("identified native face task", identityErr)
		}
		assertSemanticModelIdentity(t, frozen, identified, strings.Repeat("a", 64))
		if _, identityErr := identified.WithExecutionIdentity(strings.Repeat("a", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)); identityErr == nil {
			t.Fatal("rebound native identity")
		}
		if _, identityErr := frozen.WithExecutionIdentity(strings.Repeat("f", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)); identityErr == nil {
			t.Fatal("foreign model identity")
		}
		draft, err := frozen.DraftTask()
		if err != nil || draft.ResourceClass != "gpu" || draft.Operation != "face.detect" || draft.InputDigest != closure.InputDigest {
			t.Fatal("native task changed closure or route", err)
		}
		published, err := PublishSemanticDetectorTask(context.Background(), frozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
		if err != nil || published.Graph.Built.Count != len(got.Inputs)+1 || published.InputDigest != draft.InputDigest || published.Params != paramsRef {
			t.Fatal("SFD published closure lost inputs", err)
		}
		tampered := published.Graph.Built
		tampered.SHA256 = strings.Repeat("0", 64)
		if _, err := frozen.BindPublishedTransport(context.Background(), tampered); err == nil {
			t.Fatal("substituted transport digest accepted")
		}
		tampered = published.Graph.Built
		tampered.Count--
		if _, err := frozen.BindPublishedTransport(context.Background(), tampered); err == nil {
			t.Fatal("truncated transport accepted")
		}
		draft.ExecutionProfile[0] = 'x'
		againDraft, err := frozen.DraftTask()
		if err != nil || !json.Valid(againDraft.ExecutionProfile) {
			t.Fatal("caller mutated frozen profile")
		}
		if _, err := CompileSemanticDetectorTask(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), .8, 64, sizes[:len(sizes)-1], SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<20); err == nil {
			t.Fatal("missing catalog size accepted by task")
		}
		wrongParams := paramsRef
		wrongParams.SHA256 = strings.Repeat("e", 64)
		if _, err := CompileSemanticDetectorClosure(context.Background(), workflow, 0, binding, body, wrongParams, strings.Repeat("a", 64), .8, 64); err == nil {
			t.Fatal("wrong published Params accepted")
		}
		aliasedParams := paramsRef
		aliasedParams.ArtifactID = binding.Source.ArtifactID
		if _, err := CompileSemanticDetectorClosure(context.Background(), workflow, 0, binding, body, aliasedParams, strings.Repeat("a", 64), .8, 64); err == nil {
			t.Fatal("Params/source alias accepted")
		}
		again, err := PrepareSemanticFaceDetectParams(context.Background(), binding, body, strings.Repeat("a", 64), .8, 64)
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatal("unstable preparation", err)
		}
		wrong := binding
		wrong.CanvasHash = strings.Repeat("f", 64)
		denied, err := PrepareSemanticFaceDetectParams(context.Background(), wrong, body, strings.Repeat("a", 64), .8, 64)
		if err == nil || !reflect.DeepEqual(denied, SemanticFaceDetectPreparation{}) {
			t.Fatal("wrong canvas accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := PrepareSemanticFaceDetectParams(ctx, SessionVersionBinding{}, nil, "", .8, 64)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, SemanticFaceDetectPreparation{}) {
		t.Fatal("cancelled preparation", err)
	}
}

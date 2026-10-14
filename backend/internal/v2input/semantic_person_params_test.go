package v2input

import (
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

func TestSemanticPersonPreparationKeepsIndependentSourceChildren(t *testing.T) {
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
		got, err := PrepareSemanticPersonParams(context.Background(), binding, body, strings.Repeat("a", 64), 512)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := ResolveSemanticSourceMetadata(binding, body)
		if err != nil {
			t.Fatal(err)
		}
		paramsSum := sha256.Sum256(got.Params)
		paramsRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(paramsSum[:])}
		workflow := uuid.New()
		sizes := []SemanticInputSize{{Ref: paramsRef, Bytes: int64(len(got.Params))}}
		for _, entry := range got.Inputs {
			sizes = append(sizes, SemanticInputSize{Ref: entry.Ref, Bytes: 512})
		}
		frozen, err := CompileSemanticPersonTask(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), 512, sizes, SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		assertSemanticInventoryBoundary(t, frozen.VerifyInputInventory, got.Inputs, paramsRef)
		draft, err := frozen.DraftTask()
		if err != nil || draft.Operation != "mask.person" || draft.ResourceClass != "gpu" || draft.ParamsArtifactID != paramsRef.ArtifactID {
			t.Fatal("LRASPP task routing", err)
		}
		bound, err := frozen.WithExecutionIdentity(strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64))
		if err != nil {
			t.Fatal("person execution identity", err)
		}
		identified, err := bound.DraftTask()
		if err != nil || identified.InputDigest != draft.InputDigest || identified.ParamsArtifactID != draft.ParamsArtifactID {
			t.Fatal("identity changed inputs", err)
		}
		var original, current map[string]any
		if json.Unmarshal(draft.ExecutionProfile, &original) != nil || json.Unmarshal(identified.ExecutionProfile, &current) != nil {
			t.Fatal("identity profile")
		}
		for key, want := range map[string]string{"model_lock_id": strings.Repeat("a", 64), "model_lock_sha": strings.Repeat("a", 64), "runtime_lock_sha": strings.Repeat("b", 64), "device_profile_sha": strings.Repeat("c", 64)} {
			if current[key] != want {
				t.Fatal("missing owned identity", key)
			}
			if _, exists := original[key]; exists {
				t.Fatal("base frozen profile mutated", key)
			}
		}
		if current["resource_estimate"].(map[string]any)["scratch_bytes"].(float64) != original["resource_estimate"].(map[string]any)["scratch_bytes"].(float64)+(16<<20) {
			t.Fatal("V2 input index scratch omitted")
		}
		if _, err := bound.WithExecutionIdentity(strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)); err == nil {
			t.Fatal("identity rebound")
		}
		if _, err := frozen.WithExecutionIdentity(strings.Repeat("d", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)); err == nil {
			t.Fatal("foreign model bound")
		}
		if _, err := frozen.WithExecutionIdentity(strings.Repeat("a", 64), "bad", strings.Repeat("c", 64)); err == nil {
			t.Fatal("invalid runtime bound")
		}
		draft.ExecutionProfile[0] = ' '
		draftAgain, err := frozen.DraftTask()
		if err != nil || draftAgain.ExecutionProfile[0] != '{' {
			t.Fatal("LRASPP frozen profile mutated", err)
		}
		published, err := PublishSemanticPersonTask(context.Background(), frozen, &memoryPublisher{bodies: map[uuid.UUID][]byte{}})
		if err != nil || published.Graph.Built.Count != len(got.Inputs)+1 {
			t.Fatal("LRASPP publication", err)
		}
		changed := published.Graph.Built
		changed.Count--
		if _, err := frozen.BindPublishedTransport(context.Background(), changed); err == nil {
			t.Fatal("truncated person transport accepted")
		}
		if _, err := CompileSemanticPersonTask(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), 512, sizes[:len(sizes)-1], SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30); err == nil {
			t.Fatal("missing person catalog size accepted")
		}
		closure, err := CompileSemanticPersonClosure(context.Background(), workflow, 0, binding, body, paramsRef, strings.Repeat("a", 64), 512)
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
		if len(seen) != len(got.Inputs)+1 || seen[paramsRef.ArtifactID] != (Entry{Ref: paramsRef, Contract: "Params"}) {
			t.Fatal("person closure inventory mismatch")
		}
		for _, entry := range got.Inputs {
			if seen[entry.Ref.ArtifactID] != entry {
				t.Fatal("person source child changed")
			}
		}
		if !reflect.DeepEqual(closure.Direct, []Ref{paramsRef, binding.Source, metadata.Inputs.Pixels}) {
			t.Fatal("person bootstrap mismatch")
		}
		for _, entry := range got.Inputs {
			if _, err := CompileSemanticPersonClosure(context.Background(), workflow, 0, binding, body, entry.Ref, strings.Repeat("a", 64), 512); err == nil {
				t.Fatal("source alias accepted as person Params")
			}
		}
		badParams := paramsRef
		badParams.SHA256 = strings.Repeat("f", 64)
		if _, err := CompileSemanticPersonClosure(context.Background(), workflow, 0, binding, body, badParams, strings.Repeat("a", 64), 512); err == nil {
			t.Fatal("modified person Params accepted")
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
		var params semanticPersonParams
		if json.Unmarshal(got.Params, &params) != nil || params.Image != binding.Source || params.Model != "lraspp_mobilenet_v3_large" || params.Preprocessing != "person_guided_v1" || params.LongSide != 768 || params.GuidedTile != 512 {
			t.Fatal("person source/preprocessing changed")
		}
		again, err := PrepareSemanticPersonParams(context.Background(), binding, body, strings.Repeat("a", 64), 512)
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatal("unstable preparation", err)
		}
		wrong := binding
		wrong.CanvasHash = strings.Repeat("f", 64)
		denied, err := PrepareSemanticPersonParams(context.Background(), wrong, body, strings.Repeat("a", 64), 512)
		if err == nil || !reflect.DeepEqual(denied, SemanticPersonPreparation{}) {
			t.Fatal("wrong canvas accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := PrepareSemanticPersonParams(ctx, SessionVersionBinding{}, nil, "", 512)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, SemanticPersonPreparation{}) {
		t.Fatal("cancelled preparation", err)
	}
}

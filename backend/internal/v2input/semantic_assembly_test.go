package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func semanticAssemblyFixture(t *testing.T, count int) (SemanticAssembleParams, Ref, SemanticEditIntent, Ref, json.RawMessage, []SemanticCandidateDocument, json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	imageBody, _ := canonical.Marshal(image)
	source := semanticSnapshotBodyRef(imageBody)
	canvas, _ := canonical.Hash(image.Canvas)
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	snapshotID := uuid.New()
	binding := SessionVersionBinding{uuid.New(), source, canvas}
	intent := SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: binding, ReferenceMask: ref(), TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}
	intentBody, _ := canonical.Marshal(intent)
	params := SemanticAssembleParams{SchemaVersion: 1, Intent: semanticSnapshotBodyRef(intentBody), ModelLockSHA: strings.Repeat("b", 64), Calibration: ref()}
	items := make([]SessionSnapshotItem, count)
	candidates := make([]SemanticCandidateDocument, count)
	for n := 0; n < count; n++ {
		target := binding
		target.AssetVersionID = uuid.New()
		items[n] = SessionSnapshotItem{Ordinal: n, Binding: target, WorkingImage: source, Recipe: params.Intent}
		reason := "NO_PLANAR_CORRESPONDENCE"
		candidate := SemanticCandidate{SchemaVersion: 1, Target: target, State: "unsupported", Reason: &reason}
		if n%3 == 1 {
			mask, transform := ref(), ref()
			candidate.State = "proposed"
			candidate.Mask = &mask
			candidate.Transform = &transform
			candidate.Reason = nil
			candidate.Confidence = .8
		}
		body, _ := canonical.Marshal(candidate)
		candidateRef := semanticSnapshotBodyRef(body)
		candidates[count-1-n] = SemanticCandidateDocument{candidateRef, body, imageBody}
	}
	for _, candidate := range candidates {
		params.Candidates = append(params.Candidates, candidate.Ref)
	}
	_, snapshotBody, err := CompilePinnedSessionInputSnapshot(snapshotID, items)
	if err != nil {
		t.Fatal(err)
	}
	paramsBody, _ := canonical.Marshal(params)
	profile, err := CompileSemanticAssemblyProfile(256<<20, 32<<20, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	return params, semanticSnapshotBodyRef(paramsBody), intent, semanticSnapshotBodyRef(snapshotBody), snapshotBody, candidates, profile
}

func TestSemanticAssemblyCompilesPublishesFullFiveThousandCoverage(t *testing.T) {
	params, paramsRef, intent, snapshotRef, body, candidates, profile := semanticAssemblyFixture(t, 5000)
	workflow := uuid.New()
	frozen, err := CompileSemanticAssemblyTask(workflow, params, paramsRef, intent, snapshotRef, body, candidates, profile)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	expected, _ := TaskID(workflow, "v2.semantic.assemble", 0)
	if err != nil || draft.ID != expected || draft.ResourceClass != "cpu" {
		t.Fatal("draft")
	}
	candidates[0].Body = []byte("changed")
	params.Candidates[0].SHA256 = strings.Repeat("f", 64)
	publisher := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	prepared, err := PublishSemanticAssemblyTask(context.Background(), frozen, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Graph.Direct) != 4 || prepared.Graph.Built.Count <= 5000 || len(prepared.Graph.Built.Chunks) < 10 {
		t.Fatal("large closure transport")
	}
	var manifest Manifest
	if json.Unmarshal(publisher.bodies[prepared.Graph.Built.Manifest.ArtifactID], &manifest) != nil || manifest.EntryCount != prepared.Graph.Built.Count || manifest.TaskID != draft.ID {
		t.Fatal("manifest")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	empty := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	if _, err := PublishSemanticAssemblyTask(ctx, frozen, empty); err != context.Canceled || len(empty.bodies) != 0 {
		t.Fatal("cancellation before IO")
	}
}

func TestSemanticAssemblyRejectsCoverageContentAndResourceChanges(t *testing.T) {
	for _, variant := range []string{"candidate_bytes", "target_bytes", "intent", "snapshot", "duplicate_target", "candidate_ref", "ram", "scratch", "output", "gpu"} {
		t.Run(variant, func(t *testing.T) {
			params, paramsRef, intent, snapshotRef, body, candidates, profile := semanticAssemblyFixture(t, 2)
			switch variant {
			case "candidate_bytes":
				candidates[0].Body = append(candidates[0].Body, ' ')
			case "target_bytes":
				candidates[0].TargetBody = append(candidates[0].TargetBody, ' ')
			case "intent":
				intent.TargetSnapshotID = uuid.New()
			case "snapshot":
				snapshotRef.SHA256 = strings.Repeat("e", 64)
			case "duplicate_target":
				candidates[1] = candidates[0]
				candidates[1].Ref = semanticSnapshotBodyRef(candidates[1].Body)
				params.Candidates[1] = candidates[1].Ref
				paramsBody, _ := canonical.Marshal(params)
				paramsRef = semanticSnapshotBodyRef(paramsBody)
			case "candidate_ref":
				candidates[0].Ref.ArtifactID = uuid.New()
			default:
				var value map[string]any
				json.Unmarshal(profile, &value)
				estimate := value["resource_estimate"].(map[string]any)
				key := map[string]string{"ram": "ram_bytes", "scratch": "scratch_bytes", "output": "output_bytes", "gpu": "vram_bytes"}[variant]
				if variant == "gpu" {
					estimate[key] = 1
				} else {
					estimate[key] = estimate[key].(float64) - 1
				}
				profile, _ = canonical.Marshal(value)
			}
			if _, err := CompileSemanticAssemblyTask(uuid.New(), params, paramsRef, intent, snapshotRef, body, candidates, profile); err == nil {
				t.Fatal("invalid assembly admitted")
			}
		})
	}
}

func TestSemanticAssemblyScratchIncludesInputIndexAndPlan(t *testing.T) {
	for _, scratch := range []int64{8 << 20, 16 << 20, (32 << 20) - 1} {
		if _, err := CompileSemanticAssemblyProfile(256<<20, scratch, 8<<20); err == nil {
			t.Fatalf("insufficient index/plan scratch accepted: %d", scratch)
		}
	}
	profile, err := CompileSemanticAssemblyProfile(256<<20, 32<<20, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		ResourceEstimate struct {
			ScratchBytes int64 `json:"scratch_bytes"`
		} `json:"resource_estimate"`
	}
	if err := json.Unmarshal(profile, &value); err != nil {
		t.Fatal(err)
	}
	if value.ResourceEstimate.ScratchBytes != 32<<20 {
		t.Fatal("assembly transport scratch budget changed")
	}
}

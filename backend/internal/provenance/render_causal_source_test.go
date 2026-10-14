package provenance

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type signingRenderClosureFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderClosureEvidence, error)

func (f signingRenderClosureFunc) ReadSigningRenderClosure(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask) (SigningRenderClosureEvidence, error) {
	return f(c, w, r, t)
}
func signingCausalFixture() (CanonicalSigningTask, SigningRenderEvidence, SigningRenderClosureEvidence) {
	entries := []v2input.Entry{}
	for i, contract := range []string{"WorkingImage", "FrozenRenderRecipe", "CandidateProfile", "StyleBackgroundAsset", "ImageResult", "NpyArray", "StyleV2"} {
		var id uuid.UUID
		binary.BigEndian.PutUint64(id[8:], uint64(i+1))
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 64)}, Contract: contract})
	}
	task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1", Params: entries[1].Ref}
	task.InputDigest, _ = canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	render := SigningRenderEvidence{}
	render.taskDigest, _ = canonical.Hash(task)
	render.pixels.recipe.Scalars.Source.Source = entries[0].Ref
	render.pixels.recipe.Learning = &v2draft.FrozenRenderLearning{Model: entries[2].Ref}
	render.pixels.recipe.BackgroundAsset = &v2draft.FrozenBackgroundAsset{Source: entries[3].Ref}
	render.parents = []SigningRenderParent{{Root: entries[4].Ref}}
	render.dependencies = []v2draft.LayerRef{
		{Path: "$.recipe.source.source", Contract: "WorkingImage", Ref: entries[0].Ref, ParentRef: task.Params},
		{Path: "$.recipe.base_style", Contract: "StyleV2", Ref: entries[6].Ref, ParentRef: task.Params},
	}
	closure := SigningRenderClosureEvidence{taskDigest: render.taskDigest, inventory: v2draft.TaskInputEvidence{Entries: entries}}
	for _, node := range render.dependencies {
		closure.inventory.Occurrences = append(closure.inventory.Occurrences, v2draft.OccurrenceWitness{Node: node})
	}
	return task, render, closure
}

func TestSigningRenderCausalSourceRechecksPhysicalEvidence(t *testing.T) {
	for _, mode := range []string{"valid", "changed_render", "changed_closure", "mutated_receipt", "producer_error", "cancelled", "changed_after_read", "swapped_producers"} {
		t.Run(mode, func(t *testing.T) {
			task, render, closure := signingCausalFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			reads, closures, producerCalls := 0, 0, 0
			failure := errors.New("producer unavailable")
			source := BoundSigningRenderCausalSource{
				Evidence: signingRenderEvidenceFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderEvidence, error) {
					reads++
					if mode == "changed_render" && reads == 2 {
						render.pixels.generatedPixels++
					}
					return render, nil
				}),
				Closure: signingRenderClosureFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderClosureEvidence, error) {
					closures++
					if mode == "mutated_receipt" {
						render.pixels.recipe.Learning.Model.SHA256 = strings.Repeat("b", 64)
					}
					if mode == "changed_closure" && closures == 2 {
						closure.inventory.Occurrences[0].Node.Path = "$different"
					}
					return closure, nil
				}),
				Producers: signingProducerFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, pin v2input.Ref) (uuid.UUID, error) {
					producerCalls++
					if mode == "producer_error" {
						return uuid.Nil, failure
					}
					if mode == "changed_after_read" && closures == 2 {
						return uuid.MustParse("00000000-0000-0000-0000-000000000098"), nil
					}
					if mode == "swapped_producers" {
						first := pin == closure.inventory.Entries[0].Ref
						if closures == 2 {
							first = !first
						}
						if first {
							return uuid.MustParse("00000000-0000-0000-0000-000000000097"), nil
						}
					}
					return uuid.MustParse("00000000-0000-0000-0000-000000000099"), nil
				}),
			}
			got, err := source.read(ctx, auth.Identity{}, ReadySigningExport{}, task)
			if mode == "valid" {
				if err != nil || reads != 2 || closures != 2 || producerCalls != 8 || len(got.pixels.parents) != 1 {
					t.Fatal("causal stage incomplete", err, reads, closures, producerCalls)
				}
				closure.inventory.Entries[0].Ref.SHA256 = "changed"
				if got.inventory.Images[0].SHA256 == "changed" {
					t.Fatal("receipt aliases closure")
				}
			} else {
				if err == nil || got.inventory.Images != nil || got.pixels.inputs != nil {
					t.Fatal("partial receipt escaped", err)
				}
				if mode == "producer_error" && !errors.Is(err, failure) {
					t.Fatal("producer error hidden", err)
				}
				if mode == "cancelled" && (reads != 0 || !errors.Is(err, context.Canceled)) {
					t.Fatal("cancelled read continued", err)
				}
			}
		})
	}
}

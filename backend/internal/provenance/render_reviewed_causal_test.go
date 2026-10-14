package provenance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningReviewedRenderRequiresOneCausalAndDecisionState(t *testing.T) {
	for _, mode := range []string{"valid", "changed_render", "changed_review", "changed_closure", "changed_producer", "missing_review"} {
		t.Run(mode, func(t *testing.T) {
			task, render, closure := signingCausalFixture()
			sha := strings.Repeat("a", 64)
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
			task.Run = uuid.New()
			task.Result = uuid.New()
			task.Attempt = 1
			task.Fence = 1
			task.ResultDigest = sha
			task.Inputs = json.RawMessage(`[]`)
			task.Metrics = json.RawMessage(`{}`)
			render.pixels.root = pin()
			task.Outputs, _ = canonical.Marshal([]v2input.Ref{render.pixels.root})
			render.pixels.result = v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: pin(), ChangedMask: pin(), Report: pin()}
			render.pixels.recipe.Scalars.ModelLockSHA = sha
			render.qualification = SigningRenderQualification{ModelLockSHA: sha, RuntimeLockSHA: sha, Backend: "mps", DeviceProfileSHA: sha, DatasetSHA: sha, CalibrationSHA: sha}
			render.report.Status = "review"
			render.taskDigest, _ = canonical.Hash(task)
			closure.taskDigest = render.taskDigest
			decision := SigningRenderReview{ID: uuid.New(), Task: task.Task, Result: task.Result, Root: render.pixels.root, Report: render.pixels.result.Report, Accepted: true}
			reads, reviews, closureReads, producerReads := 0, 0, 0, 0
			producer, other := uuid.New(), uuid.New()
			source := BoundSigningRenderReviewedCausalSource{
				Causal: BoundSigningRenderCausalSource{
					Evidence: signingRenderEvidenceFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderEvidence, error) {
						reads++
						if mode == "changed_render" && reads == 3 {
							render.pixels.changedPixels++
						}
						return render, nil
					}),
					Closure: signingRenderClosureFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderClosureEvidence, error) {
						closureReads++
						if mode == "changed_closure" && closureReads >= 3 {
							closure.inventory.Occurrences[0].Node.Path = "$changed"
						}
						return closure, nil
					}),
					Producers: signingProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error) {
						producerReads++
						if mode == "changed_producer" && producerReads > 8 {
							return other, nil
						}
						return producer, nil
					}),
				},
				Reviews: signingRenderReviewFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref, bool) (*SigningRenderReview, error) {
					reviews++
					if mode == "missing_review" {
						return nil, nil
					}
					if mode == "changed_review" && reviews >= 3 {
						decision.ID = other
					}
					return &decision, nil
				}),
			}
			got, err := source.read(context.Background(), auth.Identity{}, ReadySigningExport{}, task)
			if mode == "valid" {
				if err != nil || reviews != 4 || reads != 6 || len(got.outputs.Children) != 3 || got.outputs.Action.ReviewID == nil {
					t.Fatal("reviewed causal receipt rejected", err, reads, reviews)
				}
			} else if err == nil || got.outputs.Children != nil || got.causal.inventory.Images != nil {
				t.Fatal("mixed causal/review state accepted", err)
			}
		})
	}
}

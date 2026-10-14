package provenance

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderActionRequiresExactReviewAndTaskReceipt(t *testing.T) {
	for _, mode := range []string{"pass", "review_accepted", "review_missing", "review_foreign", "review_rejected", "task_changed", "generated_missing_mask", "generated"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
			outputs, _ := canonical.Marshal([]v2input.Ref{root})
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			evidence := SigningRenderEvidence{}
			evidence.taskDigest, _ = canonical.Hash(task)
			evidence.pixels.root = root
			evidence.pixels.result.Report = v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
			evidence.pixels.recipe.Scalars.ModelLockSHA = sha
			evidence.qualification = SigningRenderQualification{ModelLockSHA: sha, RuntimeLockSHA: sha, Backend: "mps", DeviceProfileSHA: sha, DatasetSHA: sha, CalibrationSHA: sha}
			evidence.report.Status = "pass"
			var approval *SigningRenderReview
			if strings.HasPrefix(mode, "review_") {
				evidence.report.Status = "review"
				if mode != "review_missing" {
					approval = &SigningRenderReview{ID: uuid.New(), Task: task.Task, Result: task.Result, Root: root, Report: evidence.pixels.result.Report, Accepted: true}
				}
			}
			if mode == "review_foreign" {
				approval.Result = uuid.New()
			}
			if mode == "review_rejected" {
				approval.Accepted = false
			}
			if mode == "task_changed" {
				task.Result = uuid.New()
			}
			if strings.HasPrefix(mode, "generated") {
				evidence.pixels.generatedPixels = 1
			}
			if mode == "generated" {
				mask := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
				evidence.pixels.result.GeneratedMask = &mask
			}
			action, err := CompileSigningRenderAction(task, evidence, approval)
			success := mode == "pass" || mode == "review_accepted" || mode == "generated"
			if !success {
				if err == nil {
					t.Fatal("invalid render claim accepted")
				}
				return
			}
			if err != nil || action.ModelLockSHA == nil || *action.ModelLockSHA != sha || action.Generated != (mode == "generated") || (action.ReviewID != nil) != (mode == "review_accepted") {
				t.Fatal("render claims lost", err)
			}
		})
	}
}

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

func TestSigningRenderChildOutputsRequireExactPhysicalReceipt(t *testing.T) {
	for _, mode := range []string{"valid", "generated", "foreign_task", "root", "alias", "missing", "schema"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
			root := pin()
			outputs, _ := canonical.Marshal([]v2input.Ref{root})
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: pin(), Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			evidence := SigningRenderEvidence{}
			evidence.taskDigest, _ = canonical.Hash(task)
			evidence.pixels.root = root
			evidence.pixels.result = v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: pin(), ChangedMask: pin(), Report: pin()}
			generated := pin()
			switch mode {
			case "generated":
				evidence.pixels.result.GeneratedMask = &generated
			case "foreign_task":
				task.Result = uuid.New()
			case "root":
				evidence.pixels.root = pin()
			case "alias":
				evidence.pixels.result.Report = evidence.pixels.result.WorkingImage
			case "missing":
				evidence.pixels.result.ChangedMask = v2input.Ref{}
			case "schema":
				evidence.pixels.result.SchemaVersion = 2
			}
			children, err := CompileSigningRenderChildOutputs(task, evidence)
			if mode != "valid" && mode != "generated" {
				if err == nil {
					t.Fatal("invalid physical receipt accepted")
				}
				return
			}
			if err != nil || len(children) < 3 || children[0].Ref != evidence.pixels.result.WorkingImage || children[0].Root != root {
				t.Fatal("child binding lost", err)
			}
			if mode == "generated" {
				generated.ArtifactID = uuid.New()
				if len(children) != 4 || children[3].Ref == generated {
					t.Fatal("child list aliases evidence pointer")
				}
			}
		})
	}
}

type signingRenderEvidenceFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderEvidence, error)

func (f signingRenderEvidenceFunc) ReadSigningRenderEvidence(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask) (SigningRenderEvidence, error) {
	return f(c, w, r, t)
}

type signingRenderReviewFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref, bool) (*SigningRenderReview, error)

func (f signingRenderReviewFunc) ReadSigningRenderReview(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask, root, report v2input.Ref, required bool) (*SigningRenderReview, error) {
	return f(c, w, r, t, root, report, required)
}
func TestSigningRenderOutputsSharePhysicalEvidenceAndReview(t *testing.T) {
	for _, mode := range []string{"valid", "missing_review", "changed_review", "missing_child", "mutated_evidence", "mutated_task"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
			root := pin()
			outputs, _ := canonical.Marshal([]v2input.Ref{root})
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: pin(), Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			evidence := SigningRenderEvidence{}
			evidence.taskDigest, _ = canonical.Hash(task)
			evidence.pixels.root = root
			evidence.pixels.result = v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: pin(), ChangedMask: pin(), Report: pin()}
			evidence.pixels.recipe.Scalars.ModelLockSHA = sha
			evidence.qualification = SigningRenderQualification{ModelLockSHA: sha, RuntimeLockSHA: sha, Backend: "mps", DeviceProfileSHA: sha, DatasetSHA: sha, CalibrationSHA: sha}
			evidence.report.Status = "review"
			if mode == "missing_child" {
				evidence.pixels.result.ChangedMask = v2input.Ref{}
			}
			if mode == "mutated_evidence" {
				evidence.pixels.recipe.Learning = &v2draft.FrozenRenderLearning{Model: pin()}
			}
			reads, reviews := 0, 0
			source := signingRenderEvidenceFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderEvidence, error) {
				reads++
				return evidence, nil
			})
			decision := SigningRenderReview{ID: uuid.New(), Task: task.Task, Result: task.Result, Root: root, Report: evidence.pixels.result.Report, Accepted: true}
			reviewReader := signingRenderReviewFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, adapterTask CanonicalSigningTask, r, p v2input.Ref, required bool) (*SigningRenderReview, error) {
				reviews++
				if mode == "mutated_evidence" && reviews == 2 {
					evidence.pixels.recipe.Learning.Model.SHA256 = strings.Repeat("b", 64)
				}
				if mode == "mutated_task" {
					adapterTask.Metrics[0] = '['
				}

				if r != root || p != decision.Report || !required {
					t.Fatal("review scope changed")
				}
				if mode == "missing_review" {
					return nil, nil
				}
				if mode == "changed_review" && reviews == 2 {
					decision.ID = uuid.New()
				}
				return &decision, nil
			})
			result, err := ResolveSigningRenderOutputs(context.Background(), source, reviewReader, auth.Identity{}, ReadySigningExport{}, task)
			if mode == "valid" || mode == "mutated_task" {
				if string(task.Metrics) != "{}" {
					t.Fatal("adapter mutated caller task")
				}
				if err != nil || reads != 1 || reviews != 2 || len(result.Children) != 3 || result.Action.ReviewID == nil || *result.Action.ReviewID != decision.ID {
					t.Fatal("bound render outputs refused", err)
				}
				return
			}
			if err == nil || result.Children != nil {
				t.Fatal("invalid render outputs exposed")
			}
		})
	}
}

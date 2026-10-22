package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestPreviewCompilerUsesOwnedPlanWithoutApproval(t *testing.T) {
	sources, documents, model, dataset := preparedDocumentsKindFixture(t, "local_exposure")
	prepared, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil {
		t.Fatal(err)
	}
	var seed InitialTarget
	for _, value := range prepared.Targets {
		if value.State == "proposed" {
			seed = value
			break
		}
	}
	if seed.AssetVersionID == uuid.Nil {
		t.Fatal("no proposed fixture")
	}
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), Backend: "cpu",
		PolicySHA: model, Report: ref(), Calibration: ref(), ModelLockSHA: model, RuntimeLockSHA: model, DeviceProfileSHA: model, DatasetSHA: dataset}
	target := LockedTarget{ID: uuid.New(), AssetVersionID: seed.AssetVersionID, CandidateArtifactID: &seed.CandidateArtifactID, State: "proposed", Revision: 1}
	request := PreviewRequest{ExpectedRevision: "1", TargetRevision: "1", PlanSHA: prepared.PlanRef.SHA256}
	for _, d := range documents {
		if d.Source.Ref.ArtifactID == seed.CandidateArtifactID {
			request.CandidateSHA = d.Source.Ref.SHA256
		}
	}
	protection := ref()
	tenant, intent, workflow := uuid.New(), uuid.New(), uuid.New()
	for _, failure := range []string{"", "stale", "decided", "candidate", "child", "plan", "public_mutation"} {
		t.Run(failure, func(t *testing.T) {
			current := target
			r := request
			p := prepared
			switch failure {
			case "stale":
				r.TargetRevision = "2"
			case "decided":
				current.State = "approved"
			case "candidate":
				r.CandidateSHA = strings.Repeat("0", 64)
			case "child":
				child := uuid.New()
				current.ChildRunID = &child
			case "plan":
				r.PlanSHA = strings.Repeat("0", 64)
			case "public_mutation":
				p.PlanRef = ref()
				p.Intent.Kind = "remove_object"
				p.SnapshotID = uuid.New()
			}
			result, err := CompileExposurePreviewParams(tenant, intent, workflow, p, current, r, protection, pin)
			if failure != "" && failure != "public_mutation" {
				if err == nil {
					t.Fatal("stale proposal compiled")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.WorkflowID != workflow || result.WorkflowID == sources.RunID || result.Plan != prepared.PlanRef || result.Intent != prepared.IntentRef || result.SnapshotID != prepared.SnapshotID || result.Candidate.ArtifactID != seed.CandidateArtifactID || result.TargetID != target.ID {
				t.Fatal("lost private bindings")
			}
		})
	}
}

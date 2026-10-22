package semanticreview

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// All candidates retain their own source binding and become proposed through
// the existing exposure review fixture. No shown-preview authority is invented.
func mixedLegacyExposureFixture(t *testing.T) (PreparedReview, []LockedTarget, ApplyDecision) {
	t.Helper()
	sources, documents, model, dataset := preparedDocumentsKindFixture(t, "local_exposure", true)
	review, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil { t.Fatal(err) }
	var targets []LockedTarget
	for _, seed := range review.Targets {
		candidate := seed.CandidateArtifactID
		targets = append(targets, LockedTarget{ID: uuid.New(), AssetVersionID: seed.AssetVersionID, CandidateArtifactID: &candidate, State: seed.State, Revision: 1})
	}
	if len(targets) < 2 { t.Fatal("mixed exposure fixture needs two proposed targets") }
	request := ApplyDecision{PlanSHA: review.PlanRef.SHA256, ExpectedRevision: "1", TargetDecisions: []TargetDecision{
		{TargetID: targets[0].ID, Decision: "approve"},
		{TargetID: targets[1].ID, Decision: "reject"},
	}}
	return review, targets, request
}

func mixedLegacyExposureInput(t *testing.T, review PreparedReview, draft ApprovedTaskDraft) ExposureTaskInputs {
	t.Helper()
	var edit ApprovedEdit
	if err := json.Unmarshal(draft.Body, &edit); err != nil { t.Fatal(err) }
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	sha := strings.Repeat("b", 64)
	return ExposureTaskInputs{TargetID: edit.TargetID, Source: edit.Source, Reference: review.Intent.Reference, Width: 24, Height: 20,
		Dependencies: v2input.ExposureDependencies{Reference: review.Intent.Reference.Source, Source: v2input.CaptureImageInputs{Pixels: ref()}, ReferenceImage: v2input.CaptureImageInputs{Pixels: ref()}, MaskPixels: ref(), Protection: ref(), ProtectionPixels: ref()},
		OwnerCosts: ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20},
		Limits: ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000},
		Qualification: ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu"}}
}

func TestLegacyExposureMixedBatchPublishesOnlyExplicitApproval(t *testing.T) {
	for _, mode := range []string{"mixed", "reverse", "omit", "extra_rejected_input", "missing_approved_input", "substituted_rejected_input"} {
		t.Run(mode, func(t *testing.T) {
			review, targets, request := mixedLegacyExposureFixture(t)
			child := uuid.New()
			if mode == "reverse" { request.TargetDecisions[0], request.TargetDecisions[1] = request.TargetDecisions[1], request.TargetDecisions[0] }
			if mode == "omit" { request.TargetDecisions = request.TargetDecisions[:1] }
			drafts, err := CompileApprovedTaskDrafts(child, review, targets, request)
			if err != nil || len(drafts) != 1 || drafts[0].TargetID != targets[0].ID { t.Fatal("rejected/omitted target entered drafts", err) }
			input := mixedLegacyExposureInput(t, review, drafts[0])
			inputs := []ExposureTaskInputs{input}
			switch mode {
			case "extra_rejected_input":
				rejected := input; rejected.TargetID = targets[1].ID; inputs = append(inputs, rejected)
			case "missing_approved_input":
				inputs = nil
			case "substituted_rejected_input":
				inputs[0].TargetID = targets[1].ID
			}
			compiled, err := CompileApprovedExposureTasks(child, review, targets, request, inputs)
			if mode == "extra_rejected_input" || mode == "missing_approved_input" || mode == "substituted_rejected_input" {
				if err == nil || compiled != nil { t.Fatal("invalid mixed coverage produced partial tasks") }
				return
			}
			if err != nil || len(compiled) != 1 { t.Fatal("mixed exposure compilation", err) }
			published, err := PublishExposureTask(context.Background(), compiled[0], &exposureTransportPublisher{})
			if err != nil { t.Fatal(err) }
			tenant, actor := uuid.New(), uuid.New()
			proof, err := BuildDecisionProof(review, targets, request)
			if err != nil || len(proof.Approved) != 1 { t.Fatal("mixed proof", err) }
			decision := PublishedDecision{BatchID: decisionDocumentOwner(tenant, review.Sources.RunID, actor, proof.RequestSHA256), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: proof.SHA256}, Proof: proof}
			commit, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, review, targets, request, decision, inputs, []PreparedExposureTask{published})
			if err != nil || len(commit.Graphs) != 1 || len(commit.Review.Nodes) != 1 { t.Fatal("mixed commit projection", err) }
			node, err := compiled[0].DraftTask()
			if err != nil || node.ID != drafts[0].TaskID { t.Fatal("approved identity lost", err) }
			// Rejection changes the decision proof, never approved task identity,
			// dependencies, execution profile or its resource reservation estimate.
			approvalOnly := request
			approvalOnly.TargetDecisions = []TargetDecision{{TargetID: targets[0].ID, Decision: "approve"}}
			withoutRejection, err := CompileApprovedExposureTasks(child, review, targets, approvalOnly, inputs)
			if err != nil || len(withoutRejection) != 1 { t.Fatal("approval-only control", err) }
			controlNode, err := withoutRejection[0].DraftTask()
			if err != nil || controlNode.ID != node.ID || controlNode.ParamsSHA != node.ParamsSHA || string(controlNode.ExecutionProfile) != string(node.ExecutionProfile) {
				t.Fatal("rejection altered approved identity or resource profile", err)
			}
			if invalid, err := PrepareApprovedExposureCommitInputs(context.Background(), tenant, actor, child, review, targets, request, decision, inputs, []PreparedExposureTask{published, published}); err == nil || invalid.Nodes != nil || invalid.Graphs != nil {
				t.Fatal("duplicate execution escaped mixed commit projection")
			}
			if len(request.TargetDecisions) == 2 {
				for _, selected := range request.TargetDecisions {
					if selected.TargetID == targets[1].ID && (selected.Decision != "reject" || selected.Preview != nil) { t.Fatal("rejection acquired preview or approval") }
				}
			}
			if targets[1].State != "proposed" || targets[1].Revision != 1 { t.Fatal("compiler mutated unapproved target") }
		})
	}
}

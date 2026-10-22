package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"strconv"
	"testing"
)

func decisionProofFixture(t *testing.T) (PreparedReview, []LockedTarget, ApplyDecision) {
	return decisionProofKindFixture(t, "remove_object")
}

func decisionProofKindFixture(t *testing.T, kind string) (PreparedReview, []LockedTarget, ApplyDecision) {
	t.Helper()
	sources, documents, model, dataset := preparedDocumentsKindFixture(t, kind)
	prepared, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]LockedTarget, len(prepared.Targets))
	var selected uuid.UUID
	for n, seed := range prepared.Targets {
		artifact := seed.CandidateArtifactID
		targets[n] = LockedTarget{ID: uuid.New(), AssetVersionID: seed.AssetVersionID, CandidateArtifactID: &artifact, State: seed.State, Revision: 1}
		if seed.State == "proposed" {
			selected = targets[n].ID
		}
	}
	if selected == uuid.Nil {
		t.Fatal("proposed fixture required")
	}
	return prepared, targets, ApplyDecision{PlanSHA: prepared.PlanRef.SHA256, ExpectedRevision: strconv.FormatInt(sources.Revision, 10), TargetDecisions: []TargetDecision{{TargetID: selected, Decision: "approve"}}}
}

func TestDecisionProofBindsPrivatePlanAndLockedTargets(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil || len(proof.Approved) != 1 {
		t.Fatal(err)
	}
	sum := sha256.Sum256(proof.Body)
	if proof.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong body digest")
	}
	var doc decisionDocument
	if json.Unmarshal(proof.Body, &doc) != nil || doc.Targets[0].TargetID != request.TargetDecisions[0].TargetID || doc.Targets[0].Source.AssetVersionID == uuid.Nil || doc.Targets[0].Candidate.ArtifactID == uuid.Nil {
		t.Fatal("missing source binding")
	}
	prepared.PlanRef.SHA256 = "bad"
	prepared.IntentRef.ArtifactID = uuid.New()
	prepared.Targets = nil
	again, err := BuildDecisionProof(prepared, targets, request)
	if err != nil || !bytes.Equal(again.Body, proof.Body) {
		t.Fatal("mutable projection changed proof", err)
	}
	request.TargetDecisions[0].Decision = "reject"
	rejected, err := BuildDecisionProof(prepared, targets, request)
	if err != nil || len(rejected.Approved) != 0 || rejected.SHA256 == proof.SHA256 || rejected.RequestSHA256 == proof.RequestSHA256 {
		t.Fatal("decision not bound", err)
	}
}

func TestDecisionProofDeniesStaleAndSubstitutedInventory(t *testing.T) {
	for _, mutation := range []string{"candidate", "version", "duplicate", "revision", "resolved", "partial", "child"} {
		t.Run(mutation, func(t *testing.T) {
			prepared, targets, request := decisionProofFixture(t)
			selected := 0
			for n := range targets {
				if targets[n].ID == request.TargetDecisions[0].TargetID {
					selected = n
				}
			}
			switch mutation {
			case "candidate":
				id := uuid.New()
				targets[selected].CandidateArtifactID = &id
			case "version":
				targets[selected].AssetVersionID = uuid.New()
			case "duplicate":
				targets[1].ID = targets[0].ID
			case "revision":
				request.ExpectedRevision = "999"
			case "resolved":
				targets[selected].State = "rejected"
			case "partial":
				targets = targets[:len(targets)-1]
			case "child":
				id := uuid.New()
				targets[selected].ChildRunID = &id
			}
			proof, err := BuildDecisionProof(prepared, targets, request)
			if err == nil || len(proof.Body) != 0 || len(proof.Approved) != 0 {
				t.Fatal("invalid decision produced proof")
			}
		})
	}
}


func TestRejectDecisionProofDoesNotRequireOrPublishShownPreview(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	request.TargetDecisions[0].Decision = "reject"
	request.TargetDecisions[0].Preview = nil
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil || len(proof.Approved) != 0 { t.Fatal("rejection required approval resources", err) }
	var doc decisionDocument
	if err := json.Unmarshal(proof.Body, &doc); err != nil { t.Fatal(err) }
	if len(doc.Targets) != 1 || doc.Targets[0].Decision != "reject" ||
		doc.Targets[0].TargetID != request.TargetDecisions[0].TargetID ||
		doc.Targets[0].Preview != nil || bytes.Contains(proof.Body, []byte(`"preview"`)) ||
		doc.Targets[0].Source.AssetVersionID == uuid.Nil || doc.Targets[0].Candidate.ArtifactID == uuid.Nil {
		t.Fatal("rejection lost explicit lineage or published preview", string(proof.Body))
	}
	frozen := append([]byte(nil), proof.Body...)
	request.TargetDecisions[0].Decision = "approve"
	if !bytes.Equal(proof.Body, frozen) { t.Fatal("proof aliases request") }
}

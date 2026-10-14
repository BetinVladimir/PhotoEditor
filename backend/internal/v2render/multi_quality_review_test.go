package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiQualityReviewBindingsAndDecision(t *testing.T) {
	target := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	run := uuid.New()
	for _, status := range []string{"pass", "review", "fail"} {
		raw, err := json.Marshal(map[string]any{"target": target, "target_kind": "working", "stage": "master", "threshold_profile_id": "master_v1", "status": status})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		good := MultiQualityReview{Proposal: MultiProposal{WorkingImage: target, ResultID: uuid.New(), Result: v2input.Ref{ArtifactID: uuid.New(), SHA256: target.SHA256}}, QualityRun: run, QualityTask: uuid.NewSHA1(run, []byte("quality.check:master")), QualityResult: uuid.New(), ReportRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Report: raw}
		decision := MultiApplyDecision{SchemaVersion: 1, ResultID: good.Proposal.ResultID, Root: good.Proposal.Result, QualityRun: good.QualityRun, QualityResult: good.QualityResult, QualityReport: good.ReportRef, ConfirmGeometryReset: true, AcceptQualityWarnings: true}
		if err := ValidateMultiApplyDecision(decision, good); (err == nil) != (status != "fail") {
			t.Fatal("quality decision bypass", status, err)
		}
		decision.AcceptQualityWarnings = false
		if err := ValidateMultiApplyDecision(decision, good); (err == nil) != (status == "pass") {
			t.Fatal("warning confirmation bypass", status, err)
		}
		decision.ConfirmGeometryReset = false
		if ValidateMultiApplyDecision(decision, good) == nil {
			t.Fatal("geometry reset confirmation bypass")
		}
		if err := validateMultiQualityReview(good); err != nil {
			t.Fatal(status, err)
		}
		for _, change := range []func(*MultiQualityReview){
			func(v *MultiQualityReview) { v.QualityTask = uuid.New() },
			func(v *MultiQualityReview) { v.QualityResult = uuid.Nil },
			func(v *MultiQualityReview) { v.Proposal.WorkingImage.ArtifactID = uuid.New() },
			func(v *MultiQualityReview) { v.ReportRef.SHA256 = "changed" },
			func(v *MultiQualityReview) { v.Report = json.RawMessage(`{}`) },
		} {
			bad := good
			change(&bad)
			if validateMultiQualityReview(bad) == nil {
				t.Fatal("substitution admitted")
			}
		}
	}
}

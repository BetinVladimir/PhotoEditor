package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestPerspectiveQualityReviewBindingsAndDecision(t *testing.T) {
	target := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	run := uuid.New()
	for _, status := range []string{"pass", "review", "fail"} {
		raw, err := json.Marshal(map[string]any{"target": target, "target_kind": "working", "stage": "master", "threshold_profile_id": "master_v1", "status": status})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		good := PerspectiveQualityReview{Proposal: PerspectiveProposal{WorkingImage: target, ResultID: uuid.New(), Result: v2input.Ref{ArtifactID: uuid.New(), SHA256: target.SHA256}}, QualityRun: run, QualityTask: uuid.NewSHA1(run, []byte("quality.check:master")), QualityResult: uuid.New(), ReportRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Report: raw}
		decision := PerspectiveApplyDecision{SchemaVersion: 1, ResultID: good.Proposal.ResultID, Root: good.Proposal.Result, QualityRun: good.QualityRun, QualityResult: good.QualityResult, QualityReport: good.ReportRef, ConfirmGeometryReset: true, AcceptQualityWarnings: true}
		if err := ValidatePerspectiveApplyDecision(decision, good); (err == nil) != (status != "fail") {
			t.Fatal("quality decision bypass", status, err)
		}
		decision.AcceptQualityWarnings = false
		if err := ValidatePerspectiveApplyDecision(decision, good); (err == nil) != (status == "pass") {
			t.Fatal("warning confirmation bypass", status, err)
		}
		decision.ConfirmGeometryReset = false
		if ValidatePerspectiveApplyDecision(decision, good) == nil {
			t.Fatal("geometry reset confirmation bypass")
		}
		if err := validatePerspectiveQualityReview(good); err != nil {
			t.Fatal(status, err)
		}
		for _, change := range []func(*PerspectiveQualityReview){
			func(v *PerspectiveQualityReview) { v.QualityTask = uuid.New() },
			func(v *PerspectiveQualityReview) { v.QualityResult = uuid.Nil },
			func(v *PerspectiveQualityReview) { v.QualityResult = v.QualityRun },
			func(v *PerspectiveQualityReview) { v.QualityResult = v.QualityTask },
			func(v *PerspectiveQualityReview) { v.ReportRef.ArtifactID = v.Proposal.WorkingImage.ArtifactID },
			func(v *PerspectiveQualityReview) { v.ReportRef.ArtifactID = v.Proposal.Result.ArtifactID },
			func(v *PerspectiveQualityReview) { v.Proposal.WorkingImage.ArtifactID = uuid.New() },
			func(v *PerspectiveQualityReview) { v.ReportRef.SHA256 = "changed" },
			func(v *PerspectiveQualityReview) { v.Report = json.RawMessage(`{}`) },
		} {
			bad := good
			change(&bad)
			if validatePerspectiveQualityReview(bad) == nil {
				t.Fatal("substitution admitted")
			}
		}
	}
}

func TestPerspectiveQualityReviewRejectsHashBoundNoncanonicalAndEmptyBindings(t *testing.T) {
	target := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	raw, err := json.Marshal(map[string]any{"target": target, "target_kind": "working", "stage": "master", "threshold_profile_id": "master_v1", "status": "pass"})
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	value := PerspectiveQualityReview{Proposal: PerspectiveProposal{ResultID: uuid.New(), Result: target, WorkingImage: target}, QualityRun: run, QualityTask: uuid.NewSHA1(run, []byte("quality.check:master")), QualityResult: uuid.New(), ReportRef: v2input.Ref{ArtifactID: uuid.New()}, Report: raw}
	pin := func(v *PerspectiveQualityReview) {
		sum := sha256.Sum256(v.Report)
		v.ReportRef.SHA256 = hex.EncodeToString(sum[:])
	}
	pin(&value)
	if err := validatePerspectiveQualityReview(value); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PerspectiveQualityReview){
		func(v *PerspectiveQualityReview) { v.Report = append([]byte(" "), v.Report...) },
		func(v *PerspectiveQualityReview) {
			v.Report = []byte(strings.Replace(string(v.Report), `"status":"pass"`, `"status":"fail","status":"pass"`, 1))
		},
		func(v *PerspectiveQualityReview) { v.Proposal.ResultID = uuid.Nil },
		func(v *PerspectiveQualityReview) { v.Proposal.Result.ArtifactID = uuid.Nil },
		func(v *PerspectiveQualityReview) { v.Proposal.Result.SHA256 = "invalid" },
	} {
		bad := value
		mutate(&bad)
		pin(&bad)
		if err := validatePerspectiveQualityReview(bad); err == nil {
			t.Fatal("hash-bound invalid review accepted", string(bad.Report))
		}
	}
}

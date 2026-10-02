package style

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestCreateAndListRejectInvalidInputBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	tid := uuid.New()
	if _, err := s.Create(context.Background(), who, tid, "key", Create{ProjectID: uuid.Nil, Name: "x"}); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
	if _, err := s.List(context.Background(), who, tid, 101, "", "", "x"); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
	if _, err := s.List(context.Background(), who, tid, 50, "", "bad uuid", ""); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
}

func TestPatchRequiresHeaderBodyRevisionAgreement(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	_, err := s.Update(context.Background(), who, uuid.New(), uuid.New(), 2, Patch{ExpectedRevision: 1})
	if domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
}

func TestOptionalUUIDDistinguishesAbsentNullAndValue(t *testing.T) {
	var absent PairPatch
	if err := json.Unmarshal([]byte(`{"expected_revision":1}`), &absent); err != nil || absent.IgnoreMaskArtifactID.Set {
		t.Fatal("absent changed")
	}
	var cleared PairPatch
	if err := json.Unmarshal([]byte(`{"expected_revision":1,"ignore_mask_artifact_id":null}`), &cleared); err != nil || !cleared.IgnoreMaskArtifactID.Set || cleared.IgnoreMaskArtifactID.Value != nil {
		t.Fatal("null not represented")
	}
	id := uuid.New()
	var set PairPatch
	if err := json.Unmarshal([]byte(`{"expected_revision":1,"ignore_mask_artifact_id":"`+id.String()+`"}`), &set); err != nil || set.IgnoreMaskArtifactID.Value == nil || *set.IgnoreMaskArtifactID.Value != id {
		t.Fatal("uuid not represented")
	}
}

func TestVersionEndpointsFailClosedBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	if _, err := s.ListVersions(context.Background(), who, uuid.New(), uuid.New(), 0, ""); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected list error %v", err)
	}
	if _, err := s.Export(context.Background(), who, uuid.New(), uuid.New(), uuid.New()); domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("unexpected export error %v", err)
	}
}

func TestAcceptCandidateRejectsInvalidContractBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	for _, req := range []AcceptStyleRequest{{}, {CandidateRunID: uuid.New(), ExpectedStyleRevision: 1, Decision: "reject"}, {CandidateRunID: uuid.New(), ExpectedStyleRevision: 0, Decision: "accept"}} {
		if _, err := s.AcceptCandidate(context.Background(), who, uuid.New(), uuid.New(), "key", req); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("unexpected %v", err)
		}
	}
}

func TestAnalyzeRejectsInvalidContractBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	valid := AnalyzeRequest{TrainingSetID: uuid.New(), TrainingSetRevision: 1, AnalysisOptions: AnalysisOptions{AlignmentMode: "similarity_only", Seed: 42}}
	cases := []AnalyzeRequest{{}, valid, valid}
	cases[1].TrainingSetRevision = 0
	cases[2].AnalysisOptions.AlignmentMode = "affine"
	for _, req := range cases {
		if _, err := s.Analyze(context.Background(), who, uuid.New(), uuid.New(), "key", req); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("unexpected %v", err)
		}
	}
}

func TestFeedbackRequiresExplicitConsentAndExactReferences(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	valid := FeedbackRequest{TrainingSetID: uuid.New(), ExpectedRevision: 1, SourceAssetVersionID: uuid.New(), ResultAssetVersionID: uuid.New(), SessionID: "manual-edit", Confirmed: true}
	cases := []FeedbackRequest{valid, valid, valid}
	cases[0].Confirmed = false
	cases[1].ExpectedRevision = 0
	cases[2].ResultAssetVersionID = cases[2].SourceAssetVersionID
	for _, req := range cases {
		if _, err := s.AddFeedback(context.Background(), who, uuid.New(), uuid.New(), "key", req); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("unexpected %v", err)
		}
	}
}

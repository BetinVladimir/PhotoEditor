package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func importReviewDecisionFixture(kind string) ImportReviewDecisionRequest {
	ref := func(value string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(value))}
	}
	r := ImportReviewDecisionRequest{Action: "grant", Kind: kind, EntryID: uuid.New(), TargetVersionID: uuid.New(),
		Proposal: ref("proposal"), Base: ref("base"), Decision: "accept",
		Accepted: []string{rawSHA([]byte("candidate a"))}, Rejected: []string{rawSHA([]byte("candidate b"))}}
	if kind == "defects" {
		label := ref("labels")
		r.LabelMap = &label
	}
	return r
}

func TestImportReviewDecisionContractRequiresNewScopedDecision(t *testing.T) {
	for _, kind := range []string{"defects", "dust"} {
		base := importReviewDecisionFixture(kind)
		if err := validateImportReviewDecisionRequest(context.Background(), base); err != nil {
			t.Fatal("current target decision refused", kind, err)
		}
		cases := map[string]func(*ImportReviewDecisionRequest){
			"missing entry":            func(r *ImportReviewDecisionRequest) { r.EntryID = uuid.Nil },
			"missing target":           func(r *ImportReviewDecisionRequest) { r.TargetVersionID = uuid.Nil },
			"unbound proposal":         func(r *ImportReviewDecisionRequest) { r.Proposal.ArtifactID = r.Base.ArtifactID },
			"unknown kind":             func(r *ImportReviewDecisionRequest) { r.Kind = "archived" },
			"duplicate candidate":      func(r *ImportReviewDecisionRequest) { r.Rejected = append(r.Rejected, r.Accepted[0]) },
			"unknown candidate digest": func(r *ImportReviewDecisionRequest) { r.Accepted[0] = "bad" },
			"inherited decision":       func(r *ImportReviewDecisionRequest) { r.Action = "archive" },
			"revoke without epoch": func(r *ImportReviewDecisionRequest) {
				r.Action = "revoke"
				r.Decision = ""
				r.Accepted = nil
				r.Rejected = nil
			},
			"revoke with selection": func(r *ImportReviewDecisionRequest) { r.Action = "revoke"; r.ExpectedEpoch = 1 },
		}
		if kind == "defects" {
			cases["missing labels"] = func(r *ImportReviewDecisionRequest) { r.LabelMap = nil }
		} else {
			cases["foreign labels"] = func(r *ImportReviewDecisionRequest) { label := r.Base; r.LabelMap = &label }
		}
		for name, change := range cases {
			t.Run(kind+"/"+name, func(t *testing.T) {
				request := base
				request.Accepted = append([]string(nil), base.Accepted...)
				request.Rejected = append([]string(nil), base.Rejected...)
				change(&request)
				if err := validateImportReviewDecisionRequest(context.Background(), request); err == nil {
					t.Fatal("invalid imported review decision accepted")
				}
			})
		}
		revoke := base
		revoke.Action, revoke.ExpectedEpoch, revoke.Decision = "revoke", 1, ""
		revoke.Accepted, revoke.Rejected = nil, nil
		if err := validateImportReviewDecisionRequest(context.Background(), revoke); err != nil {
			t.Fatal("scoped revoke refused", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateImportReviewDecisionRequest(ctx, importReviewDecisionFixture("dust")); !errors.Is(err, context.Canceled) {
		t.Fatal("decision validation lost cancellation", err)
	}
}

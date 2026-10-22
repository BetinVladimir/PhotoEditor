package semanticreview

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestCleanupPublishersOwnIndependentMixedRequests(t *testing.T) {
	approved, rejected := uuid.New(), uuid.New()
	render := TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	evidence := TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	request := ApplyDecision{PlanSHA: strings.Repeat("a", 64), ExpectedRevision: "1", TargetDecisions: []TargetDecision{
		{TargetID: approved, Decision: "approve", Preview: &PreviewSelection{Render: render, Evidence: evidence}},
		{TargetID: rejected, Decision: "reject"},
	}}
	raw, err := canonical.Marshal(request)
	if err != nil { t.Fatal(err) }
	calls := 0
	publication := cleanupBatchPublication{
		approved: func(_ context.Context, received ApplyDecision) error {
			calls++
			received.TargetDecisions[0].Preview.Render.SHA256 = strings.Repeat("d", 64)
			received.TargetDecisions[1].Decision = "approve"
			return nil
		},
		decision: func(_ context.Context, received ApplyDecision) (PublishedDecision, error) {
			calls++
			if received.TargetDecisions[0].Preview.Render != render || received.TargetDecisions[0].Preview.Evidence != evidence || received.TargetDecisions[1].Decision != "reject" || received.TargetDecisions[1].Preview != nil {
				t.Fatal("publisher mutation crossed the frozen request boundary")
			}
			return PublishedDecision{}, nil
		},
	}
	if err := publication.publishApproved(context.Background(), raw); err != nil { t.Fatal(err) }
	if _, err := publication.publishDecision(context.Background(), raw); err != nil { t.Fatal(err) }
	if calls != 2 || request.TargetDecisions[0].Preview.Render != render || request.TargetDecisions[1].Decision != "reject" {
		t.Fatal("caller request changed or publisher skipped")
	}
}

func TestCleanupPublishersRefuseInvalidRequestBeforeCallback(t *testing.T) {
	publication := cleanupBatchPublication{
		approved: func(context.Context, ApplyDecision) error { t.Fatal("invalid approval published"); return nil },
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) { t.Fatal("invalid decision published"); return PublishedDecision{}, nil },
	}
	if err := publication.publishApproved(context.Background(), []byte(`{}`)); err == nil { t.Fatal("invalid approval accepted") }
	if _, err := publication.publishDecision(context.Background(), []byte(`{}`)); err == nil { t.Fatal("invalid decision accepted") }
	if err := (cleanupBatchPublication{}).publishApproved(context.Background(), nil); err == nil { t.Fatal("missing approval publisher accepted") }
	if _, err := (cleanupBatchPublication{}).publishDecision(context.Background(), nil); err == nil { t.Fatal("missing decision publisher accepted") }
}


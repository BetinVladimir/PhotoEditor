package workflow

import (
	"context"
	"testing"
)

func TestOperatorReviewRouterHasNoDefaultApproval(t *testing.T) {
	for _, phase := range []string{"defects_review", "dust_review", "cull_review", "composition_review", "unknown"} {
		if _, e := (OperatorReviewRouter{}).MaterializeOperatorReview(context.Background(), ApplyLease{Phase: phase}, nil, nil); e == nil {
			t.Fatalf("missing explicit %s accepted", phase)
		}
	}
}
func TestOperatorDustRequiresMeasuredScope(t *testing.T) {
	if _, e := (OperatorDustReview{}).MaterializeOperatorReview(context.Background(), ApplyLease{Phase: "dust_review"}, nil, nil); e == nil {
		t.Fatal("unbound dust review accepted")
	}
}

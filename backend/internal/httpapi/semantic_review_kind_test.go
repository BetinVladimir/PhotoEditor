package httpapi

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/semanticreview"
	"testing"
)

func TestReviewStatusKindProjectionPreservesStatusAndRejectsAuthorityLoss(t *testing.T) {
	tenant, intent := uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	base := semanticreview.ReviewStatus{RunID: uuid.New(), WorkflowID: uuid.New(), Revision: "17", State: "completed"}
	authorityLost := errors.New("current editor authority lost")
	for _, test := range []struct {
		name, kind          string
		cause               error
		wantKind, wantError bool
	}{
		{"verified", "background_cleanup", nil, true, false},
		{"missing", "", semanticreview.ErrUnavailable, false, false},
		{"revoked", "", authorityLost, false, true},
		{"invalid", "unexpected", nil, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := enrichSemanticReviewStatus(context.Background(), who, tenant, intent, base, func(_ context.Context, actual auth.Identity, tid, iid uuid.UUID) (string, error) {
				if actual.UserID != who.UserID || actual.SessionID != who.SessionID || tid != tenant || iid != intent {
					t.Fatal("scope changed")
				}
				return test.kind, test.cause
			})
			if (err != nil) != test.wantError {
				t.Fatal(err)
			}
			if test.wantError {
				return
			}
			if result.RunID != base.RunID || result.WorkflowID != base.WorkflowID || result.Revision != base.Revision || result.State != base.State {
				t.Fatal("status changed")
			}
			if (result.Kind != nil) != test.wantKind {
				t.Fatal(result.Kind)
			}
		})
	}
}
func TestReviewKindProjectionCancellationAfterReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := enrichSemanticReviewStatus(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, semanticreview.ReviewStatus{}, func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (string, error) {
		cancel()
		return "background_cleanup", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

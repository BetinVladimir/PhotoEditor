package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestShownBatchPreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareShownApplyBatch(ctx, nil, nil, "owned", auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil,
		PreparedReview{}, nil, ApplyDecision{}, nil, nil)
	if err != context.Canceled {
		t.Fatal(err)
	}
}

func TestShownBatchPreparationRejectsMissingPublicationDependencies(t *testing.T) {
	_, err := PrepareShownApplyBatch(context.Background(), nil, nil, "owned", auth.Identity{}, uuid.New(), uuid.New(), uuid.New(),
		PreparedReview{}, nil, ApplyDecision{}, nil, func(context.Context) error { t.Fatal("unexpected authority callback"); return nil })
	if err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestShownExposurePublicationAuthorityFence(t *testing.T) {
	for _, mode := range []string{"active", "missing", "before", "during", "refused", "cancelled_refusal"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var want error
			check := func(context.Context) error {
				calls++
				if mode == "during" || mode == "cancelled_refusal" {
					cancel()
				}
				if mode == "refused" || mode == "cancelled_refusal" {
					return ErrInvalid
				}
				return nil
			}
			switch mode {
			case "missing":
				check = nil
				want = ErrInvalid
			case "before":
				cancel()
				want = context.Canceled
			case "during", "cancelled_refusal":
				want = context.Canceled
			case "refused":
				want = ErrInvalid
			}
			if got := recheckShownExposurePublication(ctx, check); got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
			wantCalls := 1
			if mode == "before" || mode == "missing" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("authority calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestExposurePublishersOwnIndependentMixedRequests(t *testing.T) {
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
	publication := exposureBatchPublication{
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

func TestExposurePublishersRefuseInvalidRequestBeforeCallback(t *testing.T) {
	publication := exposureBatchPublication{
		approved: func(context.Context, ApplyDecision) error { t.Fatal("invalid approval published"); return nil },
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) { t.Fatal("invalid decision published"); return PublishedDecision{}, nil },
	}
	if err := publication.publishApproved(context.Background(), []byte(`{}`)); err == nil { t.Fatal("invalid approval accepted") }
	if _, err := publication.publishDecision(context.Background(), []byte(`{}`)); err == nil { t.Fatal("invalid decision accepted") }
	if err := (exposureBatchPublication{}).publishApproved(context.Background(), nil); err == nil { t.Fatal("missing approval publisher accepted") }
	if _, err := (exposureBatchPublication{}).publishDecision(context.Background(), nil); err == nil { t.Fatal("missing decision publisher accepted") }
}

// Exercise the production batch compiler with an actual exposure review. These
// refusals must happen before authority callbacks or any publication side effect.
func TestExposureBatchPreflightRefusesIncompleteSelection(t *testing.T) {
	for _, mode := range []string{"missing_input", "unselected_input", "reject_only", "stale_revision", "duplicate_decision", "cancelled", "missing_approved", "missing_copy", "missing_decision", "missing_authority"} {
		t.Run(mode, func(t *testing.T) {
			review, targets, request := decisionProofKindFixture(t, "local_exposure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			publication := exposureBatchPublication{
				approved: func(context.Context, ApplyDecision) error { calls++; return nil },
				copyPublisher: func(ShownPreparationInput, FrozenShownExposureTask) (v2input.Publisher, error) { calls++; return nil, ErrInvalid },
				decision: func(context.Context, ApplyDecision) (PublishedDecision, error) { calls++; return PublishedDecision{}, nil },
			}
			recheck := func(context.Context) error { calls++; return nil }
			var inputs []ShownPreparationInput
			switch mode {
			case "unselected_input":
				inputs = []ShownPreparationInput{{Selected: LockedPreviewSelection{TargetID: uuid.New()}}}
			case "reject_only":
				request.TargetDecisions[0].Decision = "reject"
			case "stale_revision":
				request.ExpectedRevision = "999"
			case "duplicate_decision":
				request.TargetDecisions = append(request.TargetDecisions, request.TargetDecisions[0])
			case "cancelled":
				cancel()
			case "missing_approved":
				publication.approved = nil
			case "missing_copy":
				publication.copyPublisher = nil
			case "missing_decision":
				publication.decision = nil
			case "missing_authority":
				recheck = nil
			}
			got, err := prepareShownExposureBatch(ctx, uuid.New(), uuid.New(), uuid.New(), review, targets, request, inputs, recheck, publication)
			if err == nil || got.Child != uuid.Nil || len(got.ShownTasks) != 0 || calls != 0 {
				t.Fatalf("incomplete batch escaped: error=%v child=%v tasks=%d callbacks=%d", err, got.Child, len(got.ShownTasks), calls)
			}
			if mode == "cancelled" && err != context.Canceled {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

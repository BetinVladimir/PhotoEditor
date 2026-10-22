package semanticreview

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestShownRetouchPublicationOwnsEachCallbackRequest(t *testing.T) {
	_, _, request := decisionProofKindFixture(t, "retouch_region")
	request.TargetDecisions[0].Preview = &PreviewSelection{
		Render: TargetDocument{ArtifactID: uuid.New(), SHA256: request.PlanSHA},
		Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: request.PlanSHA},
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DecodeApplyDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	mutate := func(got ApplyDecision) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatal("publication received a mutated acceptance")
		}
		got.TargetDecisions[0].Decision = "reject"
		got.TargetDecisions[0].TargetID = uuid.New()
		got.TargetDecisions[0].Preview.Render.SHA256 = "changed"
		got.TargetDecisions[0].Preview.Evidence.ArtifactID = uuid.New()
		calls++
	}
	result := PublishedDecision{BatchID: uuid.New()}
	publication := retouchBatchPublication{
		approved: func(_ context.Context, got ApplyDecision) error { mutate(got); return nil },
		decision: func(_ context.Context, got ApplyDecision) (PublishedDecision, error) { mutate(got); return result, nil },
	}
	for i := 0; i < 2; i++ {
		if err := publication.publishApproved(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
		got, err := publication.publishDecision(context.Background(), raw)
		if err != nil || got.BatchID != result.BatchID {
			t.Fatal("publication result was not preserved", err)
		}
	}
	decoded, err := DecodeApplyDecision(raw)
	if err != nil || !reflect.DeepEqual(decoded, want) || calls != 4 {
		t.Fatal("callback changed frozen request", err)
	}
}

func TestShownRetouchPublicationRefusesInvalidRequestBeforeCallbacks(t *testing.T) {
	_, _, request := decisionProofKindFixture(t, "retouch_region")
	valid, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"truncated", valid[:len(valid)-1]},
		{"unknown_field", []byte(`{"unexpected":true}`)},
		{"missing_decisions", []byte(`{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			publication := retouchBatchPublication{
				approved: func(context.Context, ApplyDecision) error { calls++; return nil },
				decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
					calls++
					return PublishedDecision{BatchID: uuid.New()}, nil
				},
			}
			if err := publication.publishApproved(context.Background(), tc.raw); err == nil {
				t.Fatal("invalid approved request was accepted")
			}
			got, err := publication.publishDecision(context.Background(), tc.raw)
			if err == nil || got.BatchID != uuid.Nil || calls != 0 {
				t.Fatal("invalid decision reached publication", err)
			}
		})
	}
	var missing retouchBatchPublication
	if err := missing.publishApproved(context.Background(), valid); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing approved publisher did not fail closed", err)
	}
	if _, err := missing.publishDecision(context.Background(), valid); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing decision publisher did not fail closed", err)
	}
}

func TestShownRetouchPublicationPreservesCallbackFailure(t *testing.T) {
	_, _, request := decisionProofKindFixture(t, "retouch_region")
	raw, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("publication unavailable")
	publication := retouchBatchPublication{
		approved: func(context.Context, ApplyDecision) error { return want },
		decision: func(context.Context, ApplyDecision) (PublishedDecision, error) {
			return PublishedDecision{}, want
		},
	}
	if err := publication.publishApproved(context.Background(), raw); !errors.Is(err, want) {
		t.Fatal("approved publication lost original failure", err)
	}
	if _, err := publication.publishDecision(context.Background(), raw); !errors.Is(err, want) {
		t.Fatal("decision publication lost original failure", err)
	}
}

package semanticreview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

func TestApplyServiceSeparatesUnavailableFromInvalidRequest(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, intent := uuid.New(), uuid.New()
	request := ApplyDecision{PlanSHA: strings.Repeat("a", 64), ExpectedRevision: "1", TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "reject"}}}
	for _, service := range []*ApplyService{nil, {}} {
		response, err := service.Apply(context.Background(), who, tenant, intent, "valid-key", request)
		if !errors.Is(err, ErrUnavailable) || response.Status != 0 || response.ResourceID != nil || len(response.Body) != 0 {
			t.Fatal("unconfigured service must return unavailable without a receipt", err)
		}
		invalid := request
		invalid.ExpectedRevision = "0"
		if _, err = service.Apply(context.Background(), who, tenant, intent, "valid-key", invalid); !errors.Is(err, ErrInvalid) {
			t.Fatal("malformed decision must remain invalid", err)
		}
	}
}


func TestApplyServiceCancelledShownRequestStopsBeforeReplayOrPreparation(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, intent := uuid.New(), uuid.New()
	request := ApplyDecision{PlanSHA: strings.Repeat("a",64), ExpectedRevision: "1", TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "approve", Preview: &PreviewSelection{
		Render: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("b",64)},
		Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("c",64)}}}}}
	for _, mode := range []string{"nil_service", "configured_pool", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			// A deliberately unusable pool makes accidental database access fail.
			service := &ApplyService{API: &pgxpool.Pool{}, Admission: &pgxpool.Pool{}, Prepare: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) (PreparedApply,error) {
				calls++; return PreparedApply{}, ErrInvalid
			}}
			if mode == "nil_service" { service = nil }
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if mode == "deadline" { cancel(); ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0,0)); want = context.DeadlineExceeded }
			cancel()
			got, err := service.Apply(ctx,who,tenant,intent,"shown-request",request)
			if !errors.Is(err,want) || calls != 0 || got.Status != 0 || len(got.Body) != 0 || got.ResourceID != nil {
				t.Fatal("cancelled request reached replay/preparation or returned receipt",err,calls,got)
			}
		})
	}
}

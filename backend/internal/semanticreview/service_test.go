package semanticreview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
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

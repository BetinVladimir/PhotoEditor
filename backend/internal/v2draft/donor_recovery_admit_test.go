package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type recoveryAdmissionPublisher struct{ calls int }

func (p *recoveryAdmissionPublisher) Publish(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
	p.calls++
	return artifactjson.Ref{}, errors.New("unexpected publication")
}

func TestDonorRecoveryAdmissionRejectsMissingConfigurationBeforeIO(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		db := &donorIntentDB{}
		publisher := &recoveryAdmissionPublisher{}
		service := SQLDonorParamsPublisher{DB: db, Publisher: publisher}
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		ticket, err := service.RecoverPublishAndAdmit(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
			uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), DonorRecoveryLoader{}, DonorManualProducer{}, publisher)
		cancel()
		expected := v2input.ErrInvalid
		if cancelled {
			expected = context.Canceled
		}
		if !errors.Is(err, expected) || ticket != (DonorQueuedTicket{}) || db.begins != 0 || publisher.calls != 0 {
			t.Fatalf("invalid recovery performed IO: error=%v begin=%d publish=%d", err, db.begins, publisher.calls)
		}
	}
}

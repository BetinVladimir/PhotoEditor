package workflow

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

func TestPublishSignedAssemblyParamsRejectsUnboundLeaseBeforeReadback(t *testing.T) {
	db := &signedEnvelopeReadDB{}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: uuid.New()}
	_, _, err := PublishSignedDeliveryAssemblyParams(context.Background(), db, deliveryAssemblyNoIO{},
		"documents", lease, delivery.PreparePlan{}, v2input.Ref{ArtifactID: uuid.New()})
	if err == nil || db.calls != 0 { t.Fatal("unbound signed Params reached SQL or object IO", err) }
}

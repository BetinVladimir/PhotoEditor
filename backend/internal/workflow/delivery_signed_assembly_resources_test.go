package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
)

func TestSignedAssemblyProfileRequiresCompletePublishedClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileSignedDeliveryAssemblyProfile(ctx,
		delivery.SignedAssemblyPublication{}, delivery.ExpansionLease{},
		uuid.Nil, nil, nil, nil, nil, DeliveryDerivativeLimits{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled profile accepted", err)
	}
	if _, err := CompileSignedDeliveryAssemblyProfile(context.Background(),
		delivery.SignedAssemblyPublication{}, delivery.ExpansionLease{},
		uuid.Nil, nil, nil, nil, nil, DeliveryDerivativeLimits{}); err == nil {
		t.Fatal("unsigned or incomplete profile accepted")
	}
}

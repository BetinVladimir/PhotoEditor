package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestCompileDelegatedSignerChildRefusesUnboundScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := CompileDelegatedSignerChild(ctx, ExpansionLease{}, Slot{},
		SignerDelegationScope{}, provenance.ReadySigningExport{},
		v2input.Ref{}, nil, nil,
		provenance.SigningTaskResources{}, time.Now())
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, _, err = CompileDelegatedSignerChild(context.Background(), ExpansionLease{}, Slot{},
		SignerDelegationScope{TenantID: uuid.New()}, provenance.ReadySigningExport{},
		v2input.Ref{}, nil, nil,
		provenance.SigningTaskResources{}, time.Now())
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("unbound scope accepted: %v", err) }
}

package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedSignerActivationRequiresSchedulerTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ActivateDelegatedSignerInTransaction(ctx, nil,
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{},
		DelegatedSignerTransport{})
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, err = ActivateDelegatedSignerInTransaction(context.Background(), nil,
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{},
		DelegatedSignerTransport{})
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing transaction accepted: %v", err) }
}

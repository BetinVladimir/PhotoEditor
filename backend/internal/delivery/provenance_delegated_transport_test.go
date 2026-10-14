package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedSignerTransportRequiresPublisherAndTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PublishDelegatedSignerInputs(ctx, SQLDelegatedLineageStore{},
		v2input.Ref{}, nil, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{}, nil)
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, err = PublishDelegatedSignerInputs(context.Background(), SQLDelegatedLineageStore{},
		v2input.Ref{}, nil, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing publisher accepted: %v", err) }
	err = (DelegatedSignerTransport{}).Register(context.Background(), nil,
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{})
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing transaction accepted: %v", err) }
}

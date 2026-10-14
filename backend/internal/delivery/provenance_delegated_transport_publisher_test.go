package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedSignerInputPublisherRequiresAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDelegatedSignerInputPublisher(ctx, SQLDelegatedLineageStore{},
		v2input.Ref{}, nil, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{}, nil)
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, err = NewDelegatedSignerInputPublisher(context.Background(),
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing authority accepted: %v", err) }
	var publisher *DelegatedSignerInputPublisher
	_, err = publisher.Publish(context.Background(), (provenance.FrozenSigningTask{}).Task().ID,
		0, "InputClosureChunk", nil)
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("nil publisher accepted: %v", err) }
}

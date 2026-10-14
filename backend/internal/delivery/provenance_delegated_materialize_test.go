package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedMaterializationRequiresCurrentStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := MaterializeDelegatedSignerTask(ctx, SQLDelegatedLineageStore{},
		v2input.Ref{}, nil, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{})
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	err = MaterializeDelegatedSignerTask(context.Background(),
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{})
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing store accepted: %v", err) }
}

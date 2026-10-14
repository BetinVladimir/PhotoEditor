package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDispatchCurrentDelegatedSignerRequiresInstalledDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DispatchCurrentDelegatedSigner(ctx, SQLDelegatedLineageStore{},
		v2input.Ref{}, nil, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{}, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled dispatch accepted", err)
	}
	if _, err := DispatchCurrentDelegatedSigner(context.Background(),
		SQLDelegatedLineageStore{}, v2input.Ref{}, nil,
		provenance.FrozenSigningIntent{}, provenance.FrozenSigningTask{},
		nil, nil); err == nil {
		t.Fatal("unconfigured signer dispatch accepted")
	}
}

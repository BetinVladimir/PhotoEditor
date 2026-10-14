package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestCompileCurrentDelegatedSignerChildRequiresLiveReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, _, err := CompileCurrentDelegatedSignerChild(ctx, nil,
		ExpansionLease{}, Slot{}, "photos", v2input.Ref{}, nil, nil,
		provenance.SigningTaskResources{}, time.Now())
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, _, _, _, err = CompileCurrentDelegatedSignerChild(context.Background(), nil,
		ExpansionLease{}, Slot{}, "photos", v2input.Ref{}, nil, nil,
		provenance.SigningTaskResources{}, time.Now())
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing live reader accepted: %v", err) }
}

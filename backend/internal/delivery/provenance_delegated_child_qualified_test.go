package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"photoeditor/backend/internal/v2input"
)

func TestQualifiedDelegatedSignerRequiresInstalledMeasurement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, _, err := CompileQualifiedCurrentDelegatedSignerChild(ctx,
		nil, ExpansionLease{}, Slot{}, "", v2input.Ref{}, nil,
		nil, nil, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled compile accepted", err)
	}
	if _, _, _, _, err := CompileQualifiedCurrentDelegatedSignerChild(
		context.Background(), nil, ExpansionLease{}, Slot{}, "",
		v2input.Ref{}, nil, nil, nil, time.Time{}); err == nil {
		t.Fatal("unmeasured signer accepted")
	}
}

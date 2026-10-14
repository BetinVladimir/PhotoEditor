package provenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedSigningIntentRequiresScopedEvidence(t *testing.T) {
	if _, err := PrepareDelegatedSigningIntent(context.Background(), uuid.Nil,
		ReadySigningExport{}, uuid.Nil, v2input.Ref{}, "", v2input.Ref{}, nil,
		nil, time.Time{}); err == nil { t.Fatal("unconfigured delegated intent accepted") }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PrepareDelegatedSigningIntent(ctx, uuid.Nil,
		ReadySigningExport{}, uuid.Nil, v2input.Ref{}, "", v2input.Ref{}, nil,
		nil, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestDelegatedCompilerRejectsSessionIntent(t *testing.T) {
	if _, err := CompileDelegatedSigningTask(context.Background(),
		FrozenSigningIntent{}, nil, "", SigningTaskResources{}, time.Now()); err == nil {
		t.Fatal("unconfigured delegated task accepted")
	}
}

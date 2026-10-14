package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedSigningDraftDocumentsRequireCompiledScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BuildDelegatedSigningDraftDocuments(ctx,
		SignerDelegationScope{}, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{})
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, err = BuildDelegatedSigningDraftDocuments(context.Background(),
		SignerDelegationScope{}, provenance.FrozenSigningIntent{},
		provenance.FrozenSigningTask{})
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("empty draft accepted: %v", err) }
}

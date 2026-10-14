package provenance

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestSigningRenderEvidenceSourceRequiresInstalledAuthorities(t *testing.T) {
	// No adapter may return an apparently complete receipt from zero values.
	source := BoundSigningRenderEvidenceSource{}
	result, err := source.ReadSigningRenderEvidence(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{})
	if err == nil || result.qualification.valid() || result.pixels.GeneratedPixels() != 0 {
		t.Fatal("missing authorities accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = source.ReadSigningRenderEvidence(ctx, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

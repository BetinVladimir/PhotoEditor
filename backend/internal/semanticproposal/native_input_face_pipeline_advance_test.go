package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestSavedNativeFacePipelineCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (SQLTransferSourceReader{}).advanceSavedNativeFacePipeline(ctx, nil, nil, auth.Identity{}, nativePreparationAnchor{})
	if !errors.Is(err, context.Canceled) || out.phase != "" || out.operation != "" || out.producers.detector.seal != "" || out.producers.landmarks.seal != "" || out.producers.parser.seal != "" || out.producers.skin.seal != "" {
		t.Fatal("cancelled pipeline exposed stage authority", err)
	}
}
func TestSavedNativeFacePipelineMissingInfrastructure(t *testing.T) {
	out, err := (SQLTransferSourceReader{}).advanceSavedNativeFacePipeline(context.Background(), nil, nil, auth.Identity{}, nativePreparationAnchor{})
	if err == nil || out.phase != "" || out.producers.detector.seal != "" {
		t.Fatal("missing dependencies exposed pipeline")
	}
}

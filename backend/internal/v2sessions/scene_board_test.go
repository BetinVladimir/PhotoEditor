package v2sessions

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestSceneBoardMissingDependenciesFailClosed(t *testing.T) {
	out, err := (Service{}).GetSceneBoard(context.Background(), auth.Identity{}, uuid.New(), uuid.New())
	if err == nil || out.Plan != nil || len(out.Frames) != 0 {
		t.Fatal("unavailable service exposed a board", out, err)
	}
}

func TestSceneBoardCancelledBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (Service{}).GetSceneBoard(ctx, auth.Identity{}, uuid.New(), uuid.New())
	if !errors.Is(err, context.Canceled) || out.Plan != nil || len(out.Frames) != 0 {
		t.Fatalf("cancelled board request exposed data: %+v, %v", out, err)
	}
}

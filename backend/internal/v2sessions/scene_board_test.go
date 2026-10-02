package v2sessions

import (
	"context"
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

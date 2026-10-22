package semanticproposal

import (
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestOrdinaryScenePendingRetainsPrivateCompatibility(t *testing.T) {
	pending := ordinaryScenePreparationPending()
	if !errors.Is(pending, ErrPreparationPending) || !errors.Is(pending, v2input.ErrConflict) {
		t.Fatal("pending lost explicit marker or internal recovery compatibility")
	}
	if errors.Is(v2input.ErrConflict, ErrPreparationPending) {
		t.Fatal("ordinary conflicts must not become preparation progress")
	}
}

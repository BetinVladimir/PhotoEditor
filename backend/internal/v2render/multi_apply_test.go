package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiApplyRejectsBeforeIO(t *testing.T) {
	s := MultiProposalService{}
	if _, err := s.Apply(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "optical.panorama", MultiApplyDecision{}); err == nil {
		t.Fatal("unconfigured apply admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Apply(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "optical.focus_stack", MultiApplyDecision{}); err != context.Canceled {
		t.Fatal(err)
	}
}

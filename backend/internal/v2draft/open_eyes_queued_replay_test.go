package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
)

func TestOpenEyesQueuedReplayRequiresScheduler(t *testing.T) {
	if ticket, err := (SQLOpenEyesQueuedReplay{}).Read(context.Background(),
		auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(),
		"eyes-retry", OpenEyesClientIntent{}); err == nil || ticket.RunID != uuid.Nil {
		t.Fatal("uninstalled replay returned a ticket")
	}
}

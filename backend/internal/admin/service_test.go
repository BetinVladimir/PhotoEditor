package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestAdminMethodsFailClosedWithoutIdentityOrDatabase(t *testing.T) {
	s := Service{}
	if _, err := s.ListDeadLetters(context.Background(), auth.Identity{}, 50, ""); domain.AsError(err).Code != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err := s.Replay(context.Background(), auth.Identity{}, uuid.New(), "key", ReplayRequest{}); domain.AsError(err).Code != "FORBIDDEN" {
		t.Fatal(err)
	}
}

func TestReplayValidatesBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := s.Replay(context.Background(), who, uuid.Nil, "key", ReplayRequest{}); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
}

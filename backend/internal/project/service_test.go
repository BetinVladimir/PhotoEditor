package project

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestArchiveRequiresMatchingExplicitConfirmation(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	tid, id := uuid.New(), uuid.New()
	for _, req := range []DeleteRequest{{ExpectedRevision: 1, ConfirmResourceID: uuid.New()}, {ExpectedRevision: 2, ConfirmResourceID: id}} {
		_, err := s.Archive(context.Background(), who, tid, id, 1, req)
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("expected validation failure, got %v", err)
		}
	}
}

func TestSetGrantRejectsTenantRoles(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	_, err := s.SetGrant(context.Background(), who, uuid.New(), uuid.New(), uuid.New(), 1, domain.Admin)
	if domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("expected validation failure, got %v", err)
	}
}

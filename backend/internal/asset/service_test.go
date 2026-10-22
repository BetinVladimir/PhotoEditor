package asset

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestListRejectsUnboundedAndUnknownFiltersBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	tid := uuid.New()
	for _, tc := range []struct {
		limit                   int
		project, status, search string
	}{{0, "", "", ""}, {101, "", "", ""}, {50, "bad", "", ""}, {50, uuid.Nil.String(), "", ""}, {50, "", "deleted", ""}, {50, "", "", string(make([]rune, 201))}} {
		_, err := s.List(context.Background(), who, tid, tc.limit, "", tc.project, tc.status, tc.search)
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("expected validation failure, got %v", err)
		}
	}
}

func TestDeleteRequiresMatchingExplicitConfirmation(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	tid, id := uuid.New(), uuid.New()
	for _, req := range []DeleteRequest{{ExpectedRevision: 1, ConfirmResourceID: uuid.New()}, {ExpectedRevision: 2, ConfirmResourceID: id}} {
		_, err := s.Delete(context.Background(), who, tid, id, 1, req)
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("expected validation failure, got %v", err)
		}
	}
}

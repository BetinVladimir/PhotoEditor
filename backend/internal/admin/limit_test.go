package admin

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestListDeadLettersRejectsInvalidLimit(t *testing.T) {
	s := Service{}
	who := auth.Identity{}
	for _, limit := range []int{0, -1, 101, 200} {
		_, err := s.ListDeadLetters(context.Background(), who, limit, "")
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Errorf("limit %d must return VALIDATION_FAILED, got %v", limit, err)
		}
	}
}

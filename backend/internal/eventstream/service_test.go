package eventstream

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"testing"
)

func TestListRejectsInvalidCursorAndLimitBeforeDatabase(t *testing.T) {
	s := Service{}
	who := auth.Identity{UserID: uuid.New()}
	tid := uuid.New()
	for _, tc := range []struct {
		after string
		limit int
	}{{"+1",50},{"01",50},{"-0",50},{" 1",50},{"9223372036854775808",50},{"-1", 50}, {"x", 50}, {"0", 0}, {"0", 101}} {
		_, err := s.List(context.Background(), who, tid, tc.after, tc.limit)
		if domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("expected validation failure, got %v", err)
		}
	}
}

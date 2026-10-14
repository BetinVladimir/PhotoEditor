package semanticreview

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestRunIntentRequiresIdentityBeforeStorage(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, run := uuid.New(), uuid.New()
	for _, input := range []struct {
		who         auth.Identity
		tenant, run uuid.UUID
	}{
		{auth.Identity{}, tid, run}, {who, uuid.Nil, run}, {who, tid, uuid.Nil},
		{auth.Identity{UserID: who.UserID}, tid, run},
	} {
		if _, err := ReadRunIntent(context.Background(), nil, input.who, input.tenant, input.run); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid scope reached storage", err)
		}
	}
	if _, err := ReadRunIntent(context.Background(), nil, who, tid, run); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing storage was not unavailable", err)
	}
}

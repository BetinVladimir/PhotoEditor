package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestUserMaskScopeRejectsMalformedWireBeforeIO(t *testing.T) {
	store := SQLOriginalCompositionPreflight{DB: &pgxpool.Pool{}}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, _, mask := compositionInputs()
	for _, params := range [][]byte{nil, []byte("{"), []byte("{} "), make([]byte, 1048577)} {
		if err := store.CheckUserMask(context.Background(), tenant, project, snapshot, who, params, mask); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if err := store.CheckUserMask(context.Background(), tenant, project, snapshot, who, []byte("{}"), v2input.Ref{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CheckUserMask(ctx, tenant, project, snapshot, who, []byte("{}"), mask); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

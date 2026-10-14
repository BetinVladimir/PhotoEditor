package v2draft

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestLocalPortraitOwnerScopeRejectsBeforeDatabase(t *testing.T) {
	s := SQLLocalPortraitOwnerScope{}
	for _, body := range [][]byte{nil, []byte(`{}`), []byte(`{"schema_version":1,"extra":true}`), make([]byte, 1048577)} {
		if err := s.Check(context.Background(), body); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid request accepted", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Check(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

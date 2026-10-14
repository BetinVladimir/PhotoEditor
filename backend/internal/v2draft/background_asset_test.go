package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPortableBackgroundRejectsInvalidScopeBeforeDependencies(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, portable := range []string{"", " background", "background ", string(make([]byte, 121))} {
		if _, err := (SQLBackgroundAssetResolver{}).Resolve(context.Background(), who, uuid.New(), uuid.New(), ref, portable); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("invalid portable ID reached dependencies: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLBackgroundAssetResolver{}).Resolve(ctx, who, uuid.New(), uuid.New(), ref, "background"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

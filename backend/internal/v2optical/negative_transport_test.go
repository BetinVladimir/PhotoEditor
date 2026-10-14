package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type negativePublisher struct {
	calls int
	fail  bool
	alias uuid.UUID
}

func (p *negativePublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	if p.fail {
		return v2input.Ref{}, errors.New("publication failed")
	}
	digest := sha256.Sum256(body)
	return v2input.Ref{ArtifactID: func() uuid.UUID {
		if p.alias != uuid.Nil && p.calls == 1 {
			return p.alias
		}
		return uuid.New()
	}(), SHA256: hex.EncodeToString(digest[:])}, nil
}
func TestNegativeCursorCancellation(t *testing.T) {
	cursor := &negativeEntryCursor{entries: []v2input.Entry{{Ref: v2input.Ref{ArtifactID: uuid.New()}, Contract: "Params"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := cursor.Next(ctx); ok || err != context.Canceled || cursor.index != 0 {
		t.Fatal(ok, err)
	}
	if _, ok, err := cursor.Next(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, ok, err := cursor.Next(context.Background()); ok || err != nil {
		t.Fatal(ok, err)
	}
}

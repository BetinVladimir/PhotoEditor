package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestTransferSourceReaderUnavailableAndCancelled(t *testing.T) {
	reader := SQLTransferSourceReader{}
	if body, err := reader.ReadSelectedMask(context.Background(), auth.Identity{UserID: uuid.New()}, Request{ActorID: uuid.New()}); !errors.Is(err, v2input.ErrInvalid) || body != nil {
		t.Fatal("foreign mask actor", err)
	}
	body, err := reader.Read(context.Background(), auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{})
	if !errors.Is(err, v2input.ErrInvalid) || body != nil {
		t.Fatal("unconfigured source", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if body, err := reader.ReadSelectedMask(ctx, auth.Identity{}, Request{}); !errors.Is(err, context.Canceled) || body != nil {
		t.Fatal("cancelled mask", err)
	}
	if ref, err := reader.VerifySelectedMaskPixels(ctx, auth.Identity{}, Request{}); !errors.Is(err, context.Canceled) || ref != (v2input.Ref{}) {
		t.Fatal("cancelled mask pixels", err)
	}
	if metadata, err := reader.VerifySourcePixels(ctx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}); !errors.Is(err, context.Canceled) || metadata.Width != 0 {
		t.Fatal("cancelled source pixels", err)
	}
	if protection, err := reader.ReadProtection(ctx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, v2input.Ref{}); !errors.Is(err, context.Canceled) || protection.Body != nil {
		t.Fatal("cancelled protection", err)
	}
	body, err = reader.Read(ctx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{})
	if !errors.Is(err, context.Canceled) || body != nil {
		t.Fatal("cancelled source", err)
	}
}

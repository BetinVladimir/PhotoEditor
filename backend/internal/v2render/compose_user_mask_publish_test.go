package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func userMaskDocuments(t *testing.T) ([]byte, []byte, v2input.SessionVersionBinding) {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var documents []json.RawMessage
	if err := json.Unmarshal(raw, &documents); err != nil {
		t.Fatal(err)
	}
	source, err := canonical.Bytes(documents[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	sum := sha256.Sum256(source)
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	mask, err := canonical.Marshal(map[string]any{"artifact": v2input.Ref{ArtifactID: uuid.New(), SHA256: base.Source.SHA256}, "canvas": image.Canvas, "transform_chain": image.TransformChain, "object_id": nil, "selector": "user", "confidence": 1, "model_lock_id": nil, "protected": false})
	if err != nil {
		t.Fatal(err)
	}
	return mask, source, base
}

func TestTypedUserMaskPublicationAndRejectionBeforeIO(t *testing.T) {
	mask, source, base := userMaskDocuments(t)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, document := uuid.New(), uuid.New()
	calls := 0
	publisher := compositionPublisherFunc(func(_ context.Context, actor auth.Identity, tid, version, doc uuid.UUID, body []byte) (v2input.Ref, error) {
		calls++
		if actor != who || tid != tenant || version != base.AssetVersionID || doc != document || string(body) != string(mask) {
			t.Fatal("publication binding changed")
		}
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, nil
	})
	if _, err := PublishUserCompositionMask(context.Background(), who, tenant, document, base, mask, source, publisher); err != nil || calls != 1 {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(mask, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "object_id")
	missing, _ := canonical.Marshal(fields)
	fields["object_id"] = json.RawMessage("null")
	fields["protected"] = json.RawMessage("null")
	nullProtected, _ := canonical.Marshal(fields)
	fields["protected"] = json.RawMessage("false")
	fields["confidence"] = json.RawMessage("null")
	nullConfidence, _ := canonical.Marshal(fields)
	for _, bad := range [][]byte{[]byte("{}"), append(append([]byte(nil), mask...), ' '), missing, nullProtected, nullConfidence} {
		if _, err := PublishUserCompositionMask(context.Background(), who, tenant, document, base, bad, source, publisher); !errors.Is(err, v2input.ErrInvalid) || calls != 1 {
			t.Fatal("invalid mask performed IO", err)
		}
	}
	wrong := base
	wrong.CanvasHash = base.Source.SHA256
	if _, err := ValidateUserCompositionMask(mask, source, wrong); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("wrong canvas accepted", err)
	}
	wrong = base
	wrong.Source.SHA256 = base.CanvasHash
	if _, err := ValidateUserCompositionMask(mask, source, wrong); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("wrong source SHA accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishUserCompositionMask(ctx, who, tenant, document, base, mask, source, publisher); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(err)
	}
	invalidReceipt := compositionPublisherFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error) {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: base.CanvasHash}, nil
	})
	if _, err := PublishUserCompositionMask(context.Background(), who, tenant, document, base, mask, source, invalidReceipt); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("bad receipt accepted", err)
	}
}

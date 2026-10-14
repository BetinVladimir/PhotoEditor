package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestProcessPersonOriginSignerBindsProvisionedPublicKey(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProcessPersonOriginSigner(private, other); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign public key accepted", err)
	}
	signer, err := NewProcessPersonOriginSigner(private, public)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("PhotoEditor/V30/person-mask-origin/v1\x00claim")
	signature, err := signer.SignPortablePersonOrigin(context.Background(), message)
	if err != nil || !ed25519.Verify(public, message, signature) {
		t.Fatal("process signature failed", err)
	}
	if _, err := signer.SignPortablePersonOrigin(context.Background(), nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("empty payload accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := signer.SignPortablePersonOrigin(ctx, message); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled signature accepted", err)
	}
}

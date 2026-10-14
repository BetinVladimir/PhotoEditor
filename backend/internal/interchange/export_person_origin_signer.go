package interchange

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"

	"photoeditor/backend/internal/v2input"
)

// ProcessPersonOriginSigner adapts a process-provisioned Ed25519 key handle.
// The private key never enters an archive, request, or receipt configuration.
type ProcessPersonOriginSigner struct {
	key    crypto.Signer
	public ed25519.PublicKey
}

func NewProcessPersonOriginSigner(key crypto.Signer, expected ed25519.PublicKey) (*ProcessPersonOriginSigner, error) {
	if key == nil || len(expected) != ed25519.PublicKeySize {
		return nil, v2input.ErrInvalid
	}
	actual, ok := key.Public().(ed25519.PublicKey)
	if !ok || !bytes.Equal(actual, expected) {
		return nil, v2input.ErrInvalid
	}
	return &ProcessPersonOriginSigner{key: key, public: append(ed25519.PublicKey(nil), expected...)}, nil
}

func (s *ProcessPersonOriginSigner) SignPortablePersonOrigin(ctx context.Context, body []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.key == nil || len(s.public) != ed25519.PublicKeySize || len(body) == 0 || len(body) > 16384 {
		return nil, v2input.ErrInvalid
	}
	signature, err := s.key.Sign(rand.Reader, append([]byte(nil), body...), crypto.Hash(0))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(s.public, body, signature) {
		return nil, errPortablePersonOrigin
	}
	return append([]byte(nil), signature...), nil
}

var _ PortablePersonOriginSigner = (*ProcessPersonOriginSigner)(nil)

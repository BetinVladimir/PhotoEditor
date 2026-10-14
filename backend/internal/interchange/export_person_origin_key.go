package interchange

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"

	"photoeditor/backend/internal/v2input"
)

// LoadProcessPersonOriginKey reads a local operator secret with owner-only
// permissions. The path and public digest are process configuration, never
// export request or archive data. No private bytes enter an error message.
func LoadProcessPersonOriginKey(ctx context.Context, path, publicSHA string) (crypto.Signer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validImportRefForSHA(publicSHA) {
		return nil, v2input.ErrInvalid
	}
	raw, err := readExportSecretFile(ctx, path, 128)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	if len(raw) != ed25519.SeedSize*2 {
		return nil, v2input.ErrInvalid
	}
	seed, err := hex.DecodeString(string(raw))
	if err != nil || hex.EncodeToString(seed) != string(raw) || len(seed) != ed25519.SeedSize {
		return nil, v2input.ErrInvalid
	}
	defer func() {
		for i := range seed {
			seed[i] = 0
		}
	}()
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	if hex.EncodeToString(digest[:]) != publicSHA {
		return nil, v2input.ErrInvalid
	}
	return private, ctx.Err()
}

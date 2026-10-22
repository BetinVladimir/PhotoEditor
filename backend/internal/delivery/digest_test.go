package delivery

import (
	"crypto/sha256"
	"encoding/hex"
)

// rawSHA hashes exact fixture bytes, matching immutable artifact identity.
func rawSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

package exportpreset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// DecodeFrozenConfig verifies the exact bytes addressed by an immutable config
// artifact. It never loads a mutable preset, inserts defaults or changes its hash.
// Artifact ownership/current ACL and watermark dependencies belong to admission.
func DecodeFrozenConfig(raw []byte, expectedSHA string) (Config, error) {
	invalid := func() (Config, error) { return Config{}, domain.NewError("VALIDATION_FAILED", 422) }
	if len(raw) < 2 || len(raw) > 1048576 || len(expectedSHA) != 64 {
		return invalid()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return invalid()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return invalid()
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return invalid()
	}
	exact, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(exact, raw) || ValidateConfig(config) != nil {
		return invalid()
	}
	return config, nil
}

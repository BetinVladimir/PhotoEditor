package exportpreset

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func frozenSHA(raw []byte) string { digest := sha256.Sum256(raw); return hex.EncodeToString(digest[:]) }
func TestDecodeFrozenConfig(t *testing.T) {
	raw, err := canonical.Marshal(validCreate().Config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeFrozenConfig(raw, frozenSHA(raw))
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "jpeg" || result.Quality == nil || *result.Quality != 90 {
		t.Fatal("changed config")
	}
	raw[0] = 'x'
	if result.Format != "jpeg" {
		t.Fatal("borrowed bytes")
	}
}
func TestDecodeFrozenConfigRefusesSubstitution(t *testing.T) {
	raw, err := canonical.Marshal(validCreate().Config)
	if err != nil {
		t.Fatal(err)
	}
	wrong := validCreate().Config
	wrong.Format = "webp"
	unsupported, _ := canonical.Marshal(wrong)
	extra := append([]byte(`{"unexpected":true,`), raw[1:]...)
	extra, _ = canonical.Bytes(extra)
	for _, candidate := range [][]byte{append([]byte(" "), raw...), extra, unsupported, []byte(`{"format":"jpeg","format":"png"}`), []byte(`null`)} {
		if _, err := DecodeFrozenConfig(candidate, frozenSHA(candidate)); err == nil {
			t.Fatal("invalid frozen config accepted")
		}
	}
	if _, err := DecodeFrozenConfig(raw, frozenSHA([]byte("wrong"))); err == nil {
		t.Fatal("foreign digest accepted")
	}
}
func TestValidateConfigRejectsNonfiniteNestedValues(t *testing.T) {
	config := validCreate().Config
	config.MatteRGB[0] = math.NaN()
	if ValidateConfig(config) == nil {
		t.Fatal("NaN accepted")
	}
	config = validCreate().Config
	config.Sharpen = &Sharpen{Radius: 1, Strength: math.Inf(1)}
	if ValidateConfig(config) == nil {
		t.Fatal("infinity accepted")
	}
}

package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSigningKeyFileIsolation(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.StdEncoding.EncodeToString
	data, err := json.Marshal(map[string]any{"active": "gateway-1", "seed": encode(key.Seed()), "public": map[string]string{"gateway-1": encode(public)}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "signing.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigningKeys(path, "gateway")
	if err != nil || signer.Active != "gateway-1" || !signer.Private.Equal(key) {
		t.Fatal("key load failed", err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSigningKeys(path, "gateway"); err == nil {
		t.Fatal("readable private key accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "link")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSigningKeys(link, "gateway"); err == nil {
		t.Fatal("symlink accepted")
	}
	duplicated := append([]byte(`{"active":"other",`), data[1:]...)
	if err = os.WriteFile(path, duplicated, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSigningKeys(path, "gateway"); err == nil {
		t.Fatal("duplicate active accepted")
	}
	if err = os.WriteFile(path, []byte(`{"active":"a","seed":"bad","public":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSigningKeys(path, "gateway"); err == nil {
		t.Fatal("malformed seed accepted")
	}
}
func TestGatewayRequiresExplicitLimitsAndCredentials(t *testing.T) {
	for _, key := range []string{"GATEWAY_CLIENT_CA_FILE", "GATEWAY_CERT_FILE", "GATEWAY_KEY_FILE", "GATEWAY_SIGNING_KEYS_FILE"} {
		t.Setenv(key, "operator-file")
	}
	t.Setenv("GATEWAY_MAX_UPLOAD_BYTES", "1048576")
	t.Setenv("GATEWAY_MAX_DOWNLOAD_BYTES", "2097152")
	c, err := ConfigFromEnv("postgres://local/test", "127.0.0.1:8443", true)
	if err != nil || c.MaxUploadBytes != 1048576 {
		t.Fatal(err)
	}
	for _, value := range []string{"", "0", "-1", "9007199254740992", "1.5"} {
		t.Setenv("GATEWAY_MAX_UPLOAD_BYTES", value)
		if _, err := ConfigFromEnv("postgres://local/test", "127.0.0.1:8443", true); err == nil {
			t.Fatal("invalid upload limit accepted")
		}
	}
}

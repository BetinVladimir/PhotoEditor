package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestEmail(t *testing.T) {
	for raw, want := range map[string]string{"  User@EXAMPLE.COM ": "user@example.com", "A@пример.рф": "a@xn--e1afmkfd.xn--p1ai"} {
		got, err := NormalizeEmail(raw)
		if err != nil || got != want {
			t.Fatalf("%s %s %v", raw, got, err)
		}
	}
	for _, s := range []string{"a@@b", "a b@x", "a\n@x", "a@-x", "a@x..y", ".a@x", "a..b@x", "a@x/evil", "<a>@x"} {
		if _, err := NormalizeEmail(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestMACDomainAndBoundaries(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	if Equal(MAC(key, "otp", "ab", "c"), MAC(key, "otp", "a", "bc")) || Equal(MAC(key, "otp", "ab"), MAC(key, "csrf", "ab")) {
		t.Fatal("MAC collision")
	}
}
func TestEnvelope(t *testing.T) {
	k := KeyRing{Active: "k1", Keys: map[string][]byte{"k1": bytes.Repeat([]byte{1}, 32)}}
	id, a, err := k.Seal([]byte("secret"), []byte("challenge1"))
	if err != nil {
		t.Fatal(err)
	}
	_, b, _ := k.Seal([]byte("secret"), []byte("challenge1"))
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	plain, err := k.Open(id, a, []byte("challenge1"))
	if err != nil || string(plain) != "secret" {
		t.Fatal(err)
	}
	if _, err = k.Open(id, a, []byte("challenge2")); err == nil {
		t.Fatal("AAD not bound")
	}
	a[len(a)-1] ^= 1
	if _, err = k.Open(id, a, []byte("challenge1")); err == nil {
		t.Fatal("tamper accepted")
	}
}
func TestJWTValidation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Truncate(time.Second)
	s := TokenSigner{Active: "k1", Private: priv, Public: map[string]ed25519.PublicKey{"k1": pub}, Issuer: "photo", Audience: "api", Now: func() time.Time { return now }}
	raw, err := s.Issue(uuid.New(), uuid.New(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(raw); err != nil {
		t.Fatal(err)
	}
	s.Audience = "other"
	if _, err = s.Verify(raw); err == nil {
		t.Fatal("audience")
	}
	s.Audience = "api"
	now = now.Add(5 * time.Minute)
	if _, err = s.Verify(raw); err == nil {
		t.Fatal("expiry")
	}
	bad := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "photo", "aud": "api", "exp": now.Add(time.Hour).Unix()})
	bad.Header["kid"] = "k1"
	signed, _ := bad.SignedString([]byte(pub))
	if _, err = s.Verify(signed); err == nil {
		t.Fatal("algorithm confusion")
	}
}
func TestSecretAndCode(t *testing.T) {
	s, err := NewSecret(rand.Reader)
	if err != nil || len(s) != 43 {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		s, err = NewOTP(rand.Reader)
		if err != nil || !ValidOTP(s) {
			t.Fatal(s, err)
		}
	}
}

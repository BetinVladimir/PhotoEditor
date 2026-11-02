package auth

import (
	"bytes"
	"encoding/base64"
	"github.com/google/uuid"
	"testing"
)

func TestRecentAuthCodeAndReceiptHaveSeparateDomainsAndBindings(t *testing.T) {
	service := &Service{Pepper: bytes.Repeat([]byte{7}, 32)}
	id := uuid.New()
	scope := recentAuthScope{Who: Identity{UserID: uuid.New(), SessionID: uuid.New(), Epoch: 4}, Purpose: "delete_profile"}
	fresh := service.recentOTPHash(id, "fixture@example.invalid", scope, "123456")
	if Equal(fresh, MAC(service.Pepper, "otp", id.String(), "fixture@example.invalid", "123456")) {
		t.Fatal("login and sensitive-action codes share a domain")
	}
	for _, mutate := range []func(*recentAuthScope){func(v *recentAuthScope) { v.Purpose = "resolve_ownership" }, func(v *recentAuthScope) { v.Who.UserID = uuid.New() }, func(v *recentAuthScope) { v.Who.SessionID = uuid.New() }, func(v *recentAuthScope) { v.Who.Epoch++ }} {
		changed := scope
		mutate(&changed)
		if Equal(fresh, service.recentOTPHash(id, "fixture@example.invalid", changed, "123456")) {
			t.Fatal("OTP binding changed without changing HMAC")
		}
	}
	receipt := OTPReceiptRequest{Key: "immutable-key", Bootstrap: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))}
	binding := service.recentReceiptBinding(scope, id, "123456", receipt)
	changed := receipt
	changed.Key = "different-key"
	if Equal(binding, service.recentReceiptBinding(scope, id, "123456", changed)) {
		t.Fatal("recovery not bound to request key")
	}
	if Equal(service.recentReceiptKeys().Keys["recent-auth-receipt-v1"], service.otpReceiptKey().Keys["otp-verify-receipt-v1"]) {
		t.Fatal("receipt encryption domains overlap")
	}
}
func TestRecentAuthReceiptAndOpaqueGrantRejectMalformedValues(t *testing.T) {
	nonce := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	raw := uuid.NewString() + "." + nonce
	id, hash, err := ParseRecentAuthGrant(raw)
	if err != nil || id == uuid.Nil || len(hash) != 32 || !Equal(hash, HashSecret(raw)) {
		t.Fatal("valid grant failed")
	}
	for _, value := range []string{"", nonce, uuid.Nil.String() + "." + nonce, uuid.NewString() + "." + nonce + "=", raw + ".extra", uuid.NewString() + "." + "plain-client-hash"} {
		if _, _, err := ParseRecentAuthGrant(value); err == nil {
			t.Fatal("malformed grant accepted")
		}
	}
	for _, r := range []OTPReceiptRequest{{Key: "", Bootstrap: nonce}, {Key: "key", Bootstrap: "short"}, {Key: "key", Bootstrap: nonce + "="}} {
		if recentReceiptValid(r) {
			t.Fatal("malformed recovery scope accepted")
		}
	}
}

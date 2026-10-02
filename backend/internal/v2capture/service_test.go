package v2capture

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestCanonicalDevicePublicKey(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	encoded := base64.RawURLEncoding.EncodeToString(public)
	if !validPublicKey(encoded) {
		t.Fatal("canonical Ed25519 public key rejected")
	}
	for _, bad := range []string{"", encoded + "=", encoded[:42] + "!", encoded[:42] + "/"} {
		if validPublicKey(bad) {
			t.Fatalf("invalid public key accepted: %q", bad)
		}
	}
}

func TestDeviceLabelBounds(t *testing.T) {
	for _, label := range []string{"", "line\nbreak", string(make([]byte, 121))} {
		if validLabel(label) {
			t.Fatalf("invalid label accepted: %q", label)
		}
	}
	if !validLabel("Studio camera 1") {
		t.Fatal("ordinary device label rejected")
	}
}

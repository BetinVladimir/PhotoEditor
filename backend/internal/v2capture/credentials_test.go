package v2capture

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func testCredentials(now time.Time) Credentials {
	seed := make([]byte, ed25519.SeedSize)
	private := ed25519.NewKeyFromSeed(seed)
	return Credentials{Pepper: make([]byte, 32), Origin: "https://studio.example.test",
		Now: func() time.Time { return now }, Signer: auth.TokenSigner{
			Active: "key1", Private: private,
			Public: map[string]ed25519.PublicKey{"key1": private.Public().(ed25519.PublicKey)},
			Issuer: "photo", Audience: "api", Now: func() time.Time { return now },
		}}
}

func TestChallengeAEADAndSignatureBinding(t *testing.T) {
	credentials := testCredentials(time.Unix(1000, 0))
	tenantID, deviceID, challengeID := uuid.New(), uuid.New(), uuid.New()
	nonce, version, sealed, hash, err := credentials.NewChallenge(tenantID, deviceID, challengeID, 2)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(nonce)
	digest := sha256.Sum256(decoded)
	if hash != hex.EncodeToString(digest[:]) {
		t.Fatal("nonce hash mismatch")
	}
	opened, err := credentials.OpenChallenge(tenantID, deviceID, challengeID, 2, version, sealed)
	if err != nil || opened != nonce {
		t.Fatal("challenge nonce did not round trip")
	}
	if _, err = credentials.OpenChallenge(tenantID, deviceID, challengeID, 3, version, sealed); err == nil {
		t.Fatal("old challenge accepted after credential epoch change")
	}
	payload, err := SignedChallenge(deviceID, challengeID, nonce, 2, credentials.Origin)
	if err != nil {
		t.Fatal(err)
	}
	public := credentials.Signer.Public["key1"]
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(credentials.Signer.Private, payload))
	if err = VerifyChallengeSignature(base64.RawURLEncoding.EncodeToString(public), signature, payload); err != nil {
		t.Fatal(err)
	}
	other, _ := SignedChallenge(deviceID, challengeID, nonce, 2, "https://other.example.test")
	if VerifyChallengeSignature(base64.RawURLEncoding.EncodeToString(public), signature, other) == nil {
		t.Fatal("signature accepted for another server origin")
	}
}

func TestCaptureTokenAudienceAndExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	credentials := testCredentials(now)
	tenantID, projectID, deviceID := uuid.New(), uuid.New(), uuid.New()
	raw, err := credentials.IssueToken(tenantID, projectID, deviceID, 4)
	if err != nil {
		t.Fatal(err)
	}
	gotDevice, claims, err := credentials.VerifyToken(raw)
	if err != nil || gotDevice != deviceID || claims.TenantID != tenantID ||
		claims.ProjectID != projectID || claims.Epoch != 4 {
		t.Fatal("capture token claims mismatch", err)
	}
	if _, err = credentials.Signer.Verify(raw); err == nil {
		t.Fatal("user API accepted capture token")
	}
	if _, err = (PublicService{Credentials: credentials}).AuthenticateDevice(
		context.Background(), "Bearer "+raw, uuid.New()); err == nil {
		t.Fatal("capture token accepted for another tenant")
	}
	credentials.Now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, _, err = credentials.VerifyToken(raw); err == nil {
		t.Fatal("expired capture token accepted")
	}
}

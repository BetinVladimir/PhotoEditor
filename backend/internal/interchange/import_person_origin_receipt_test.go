package interchange

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func portablePersonOriginFixture(t *testing.T) ([]byte, PortablePersonOriginExpectation, map[PortablePersonOriginKey]ed25519.PublicKey) {
	t.Helper()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	claim := PortablePersonOriginClaim{SchemaVersion: 1, IssuerID: uuid.New(), Source: source,
		Recipe: ref(), Override: ref(), MaskRef: ref(), MaskPixels: ref(),
		ProducerTaskID: uuid.New(), ProducerAttempt: 2, ProducerFence: 3,
		Operation: "mask.person", OperationVersion: "1",
		ModelLockSHA:        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		QualificationReport: ref()}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(public)
	keyID := hex.EncodeToString(digest[:])
	body, err := canonical.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	envelope := PortablePersonOriginEnvelope{SchemaVersion: 1, Claim: claim, KeyID: keyID,
		SignatureHex: hex.EncodeToString(ed25519.Sign(private, append([]byte(portablePersonOriginDomain), body...)))}
	raw, err := canonical.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	expected := PortablePersonOriginExpectation{Source: source, Recipe: claim.Recipe,
		Override: claim.Override, MaskRef: claim.MaskRef, MaskPixels: claim.MaskPixels,
		ModelLockSHA: claim.ModelLockSHA}
	return raw, expected, map[PortablePersonOriginKey]ed25519.PublicKey{
		{IssuerID: claim.IssuerID, KeyID: keyID}: public}
}

func TestPortablePersonOriginRequiresTrustedSignatureAndExactScope(t *testing.T) {
	raw, expected, keys := portablePersonOriginFixture(t)
	claim, err := VerifyPortablePersonOrigin(raw, expected, keys)
	if err != nil || claim.MaskPixels != expected.MaskPixels {
		t.Fatalf("valid receipt refused: %v", err)
	}
	t.Run("scope", func(t *testing.T) {
		changed := expected
		changed.MaskPixels.ArtifactID = uuid.New()
		if _, err := VerifyPortablePersonOrigin(raw, changed, keys); err == nil {
			t.Fatal("foreign mask admitted")
		}
	})
	t.Run("trust", func(t *testing.T) {
		if _, err := VerifyPortablePersonOrigin(raw, expected, map[PortablePersonOriginKey]ed25519.PublicKey{}); err == nil {
			t.Fatal("untrusted issuer admitted")
		}
	})
	t.Run("signature", func(t *testing.T) {
		var envelope PortablePersonOriginEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.SignatureHex = strings.Repeat("0", 128)
		changed, err := canonical.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyPortablePersonOrigin(changed, expected, keys); err == nil {
			t.Fatal("mutated receipt admitted")
		}
	})
	t.Run("canonical", func(t *testing.T) {
		if _, err := VerifyPortablePersonOrigin(append(raw, ' '), expected, keys); err == nil {
			t.Fatal("noncanonical receipt admitted")
		}
	})
}

package v2region

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func authoredMediaWireFixture() AuthoredRegionMedia {
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	body := []byte{0xff, 0xd8, 0xff, 0xe0}
	id := uuid.New()
	root := uuid.New()
	intent := digest([]byte("intent"))
	derivative := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "request_id": id, "intent_sha256": intent, "derivative": derivative, "bytes": len(body), "media_type": "image/jpeg", "review_required": true})
	return AuthoredRegionMedia{
		Proof:      AuthoredRegionMediaProof{Tenant: uuid.New(), Asset: uuid.New(), Actor: uuid.New(), Session: uuid.New(), Revision: 1, Canonical: AuthoredCropVerification{Request: id, Run: uuid.New(), Task: uuid.New(), Result: uuid.New(), Root: root, IntentSHA: intent, RootSHA: digest(manifest), Attempt: 1, Fence: 1}},
		Descriptor: AuthoredRegionMediaDescriptor{SchemaVersion: 1, Request: id, Revision: 1, IntentSHA: intent, Manifest: v2input.Ref{ArtifactID: root, SHA256: digest(manifest)}, ManifestBytes: manifest, Derivative: derivative, Bytes: int64(len(body)), MediaType: "image/jpeg", ReviewRequired: true}, Body: body,
	}
}

func TestAuthoredMediaFreezeCopiesExactBuffersAndRetainsReview(t *testing.T) {
	m := authoredMediaWireFixture()
	f, err := FreezeAuthoredRegionMedia(m)
	if err != nil {
		t.Fatal(err)
	}
	m.Body[0] = 0
	m.Descriptor.ManifestBytes[0] = '!'
	if f.Body[0] != 0xff || f.Descriptor.ManifestBytes[0] != '{' || !f.Descriptor.ReviewRequired {
		t.Fatal("shared mutable buffers or lost review requirement")
	}
}
func TestAuthoredMediaFreezeRefusesSubstitutionAndOversizedBuffers(t *testing.T) {
	cases := map[string]func(*AuthoredRegionMedia){
		"body hash":        func(m *AuthoredRegionMedia) { m.Body[3] ^= 1 },
		"manifest hash":    func(m *AuthoredRegionMedia) { m.Descriptor.ManifestBytes[1] = '!' },
		"request":          func(m *AuthoredRegionMedia) { m.Descriptor.Request = uuid.New() },
		"revision":         func(m *AuthoredRegionMedia) { m.Descriptor.Revision++ },
		"intent":           func(m *AuthoredRegionMedia) { m.Descriptor.IntentSHA = m.Proof.Canonical.RootSHA },
		"mime":             func(m *AuthoredRegionMedia) { m.Descriptor.MediaType = "text/html" },
		"bytes":            func(m *AuthoredRegionMedia) { m.Descriptor.Bytes++ },
		"derivative bound": func(m *AuthoredRegionMedia) { m.Body = make([]byte, AuthoredRegionDerivativeMaxBytes+1) },
		"manifest bound": func(m *AuthoredRegionMedia) {
			m.Descriptor.ManifestBytes = make([]byte, AuthoredRegionManifestMaxBytes+1)
		},
		"attempt":       func(m *AuthoredRegionMedia) { m.Proof.Canonical.Attempt = 0 },
		"fence":         func(m *AuthoredRegionMedia) { m.Proof.Canonical.Fence = 0 },
		"session":       func(m *AuthoredRegionMedia) { m.Proof.Session = uuid.Nil },
		"root mismatch": func(m *AuthoredRegionMedia) { m.Proof.Canonical.RootSHA = m.Proof.Canonical.IntentSHA },
		"review flag":   func(m *AuthoredRegionMedia) { m.Descriptor.ReviewRequired = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := authoredMediaWireFixture()
			mutate(&m)
			if _, err := FreezeAuthoredRegionMedia(m); err == nil {
				t.Fatal("accepted invalid media")
			}
		})
	}
}

func TestAuthoredMediaRejectsDuplicateAndNoncanonicalManifest(t *testing.T) {
	for _, kind := range []string{"duplicate", "whitespace", "root id"} {
		t.Run(kind, func(t *testing.T) {
			m := authoredMediaWireFixture()
			switch kind {
			case "duplicate":
				m.Descriptor.ManifestBytes = append([]byte(`{"schema_version":1,`), m.Descriptor.ManifestBytes[1:]...)
			case "whitespace":
				m.Descriptor.ManifestBytes = append([]byte(" "), m.Descriptor.ManifestBytes...)
			case "root id":
				m.Descriptor.Manifest.ArtifactID = uuid.New()
			}
			sum := sha256.Sum256(m.Descriptor.ManifestBytes)
			m.Descriptor.Manifest.SHA256 = hex.EncodeToString(sum[:])
			m.Proof.Canonical.RootSHA = m.Descriptor.Manifest.SHA256
			if _, err := FreezeAuthoredRegionMedia(m); err == nil {
				t.Fatal("accepted ambiguous or substituted root")
			}
		})
	}
}

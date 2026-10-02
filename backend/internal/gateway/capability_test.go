package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"testing"
	"time"
)

func fixture(t *testing.T) (CapabilitySigner, TaskIdentity, *x509.Certificate, time.Time) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0).UTC()
	s := CapabilitySigner{Active: "test-key", Private: key, Public: map[string]ed25519.PublicKey{"test-key": pub}, Issuer: "photo-gateway", Now: func() time.Time { return now }}
	id := TaskIdentity{TenantID: uuid.New(), RunID: uuid.New(), TaskID: uuid.New(), WorkerID: uuid.New(), Attempt: 2, Fence: 9}
	cert := &x509.Certificate{Raw: []byte("test-only certificate identity"), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	return s, id, cert, now
}
func TestCapabilityBindsTaskFenceAndCertificate(t *testing.T) {
	s, id, cert, now := fixture(t)
	raw, err := s.Issue(id, cert, now.Add(40*time.Second), now.Add(time.Hour), []string{ReadObjects, WriteOutputs})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Verify(raw, cert, id, ReadObjects)
	if err != nil || claims.ExpiresAt.Unix() != now.Add(40*time.Second).Unix() {
		t.Fatal("lease expiry not enforced", err)
	}
	changed := id
	changed.Fence++
	if _, err = s.Verify(raw, cert, changed, ReadObjects); err == nil {
		t.Fatal("accepted stale fence")
	}
	changed = id
	changed.TenantID = uuid.New()
	if _, err = s.Verify(raw, cert, changed, ReadObjects); err == nil {
		t.Fatal("accepted cross-tenant capability")
	}
	other := *cert
	other.Raw = []byte("another client")
	if _, err = s.Verify(raw, &other, id, ReadObjects); err == nil {
		t.Fatal("accepted copied bearer on another certificate")
	}
	s.Now = func() time.Time { return now.Add(40 * time.Second) }
	if _, err = s.Verify(raw, cert, id, ReadObjects); err == nil {
		t.Fatal("accepted expired capability")
	}
}
func TestScopeAndSigningKeyIsolation(t *testing.T) {
	s, id, cert, now := fixture(t)
	raw, err := s.Issue(id, cert, now.Add(time.Hour), now.Add(time.Hour), []string{ReadObjects})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(raw, cert, id, WriteOutputs); err == nil {
		t.Fatal("read token permitted upload")
	}
	for _, scopes := range [][]string{{}, {"admin"}, {ReadObjects, ReadObjects}} {
		if _, err = s.Issue(id, cert, now.Add(time.Hour), now.Add(time.Hour), scopes); err == nil {
			t.Fatal("invalid scopes")
		}
	}
	claims, err := s.Verify(raw, cert, id, ReadObjects)
	if err != nil || claims.ExpiresAt.Unix() != now.Add(time.Minute).Unix() {
		t.Fatal("TTL exceeds 60s", err)
	}
	other, _, _, _ := fixture(t)
	if _, err = other.Verify(raw, cert, id, ReadObjects); err == nil {
		t.Fatal("accepted foreign signer")
	}
	s.Public = map[string]ed25519.PublicKey{}
	if _, err = s.Issue(id, cert, now.Add(time.Minute), now.Add(time.Minute), []string{ReadObjects}); err == nil {
		t.Fatal("unpublished signing key")
	}
}
func TestVerifiedPeerRequiresVerifiedTLSChain(t *testing.T) {
	_, _, cert, _ := fixture(t)
	for _, state := range []*tls.ConnectionState{nil, {PeerCertificates: []*x509.Certificate{cert}}, {HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}}} {
		if _, err := VerifiedPeer(state); err == nil {
			t.Fatal("accepted unverified TLS identity")
		}
	}
	state := &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	if _, err := VerifiedPeer(state); err != nil {
		t.Fatal(err)
	}
}

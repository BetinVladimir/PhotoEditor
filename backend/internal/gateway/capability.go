// Package gateway implements the internal, task-scoped object boundary.
package gateway

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"time"
)

var ErrCapability = errors.New("invalid task capability")

const Audience = "photo-internal"
const ReadObjects = "objects:read"
const WriteOutputs = "outputs:write"

type TaskIdentity struct {
	TenantID uuid.UUID `json:"tenant_id"`
	RunID    uuid.UUID `json:"run_id"`
	TaskID   uuid.UUID `json:"task_id"`
	WorkerID uuid.UUID `json:"worker_id"`
	Attempt  int       `json:"attempt"`
	Fence    int64     `json:"fence,string"`
}

func (i TaskIdentity) valid() bool {
	return i.TenantID != uuid.Nil && i.RunID != uuid.Nil && i.TaskID != uuid.Nil && i.WorkerID != uuid.Nil && i.Attempt >= 1 && i.Attempt <= 6 && i.Fence > 0
}

type Claims struct {
	TaskIdentity
	CertificateSHA256 string   `json:"certificate_sha256"`
	Scopes            []string `json:"scopes"`
	jwt.RegisteredClaims
}

type CapabilitySigner struct {
	Active  string
	Private ed25519.PrivateKey
	Public  map[string]ed25519.PublicKey
	Issuer  string
	Now     func() time.Time
}

func (s CapabilitySigner) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func certificateHash(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}

// VerifiedPeer accepts only the leaf proven by the TLS handshake, never a
// reverse-proxy header or an unverified PeerCertificates entry.
func VerifiedPeer(state *tls.ConnectionState) (*x509.Certificate, error) {
	if state == nil || !state.HandshakeComplete || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
		return nil, ErrCapability
	}
	leaf := state.PeerCertificates[0]
	if leaf == nil {
		return nil, ErrCapability
	}
	for _, chain := range state.VerifiedChains {
		if len(chain) > 0 && chain[0] != nil && bytes.Equal(chain[0].Raw, leaf.Raw) && !leaf.IsCA {
			return leaf, nil
		}
	}
	return nil, ErrCapability
}

// Issue is called only after DB authorization confirms this certificate's
// registered worker and current task lease. A signed token never replaces those
// live checks at GET/PUT time, so revocation/lease loss takes effect immediately.
func (s CapabilitySigner) Issue(id TaskIdentity, cert *x509.Certificate, leaseUntil, deadline time.Time, scopes []string) (string, error) {
	if !id.valid() || cert == nil || cert.IsCA || len(cert.Raw) == 0 || s.Issuer == "" || s.Active == "" || len(s.Private) != ed25519.PrivateKeySize {
		return "", ErrCapability
	}
	public, ok := s.Public[s.Active]
	if !ok || !bytes.Equal(public, s.Private.Public().(ed25519.PublicKey)) {
		return "", ErrCapability
	}
	if !validScopes(scopes) {
		return "", ErrCapability
	}
	now := s.now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return "", ErrCapability
	}
	expiry := now.Add(60 * time.Second)
	for _, limit := range []time.Time{leaseUntil, deadline, cert.NotAfter} {
		if limit.Before(expiry) {
			expiry = limit
		}
	}
	if expiry.Unix() <= now.Unix() {
		return "", ErrCapability
	}
	c := Claims{TaskIdentity: id, CertificateSHA256: certificateHash(cert), Scopes: append([]string(nil), scopes...), RegisteredClaims: jwt.RegisteredClaims{
		Issuer: s.Issuer, Subject: id.WorkerID.String(), Audience: jwt.ClaimStrings{Audience}, ID: uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expiry)}}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	token.Header["kid"] = s.Active
	return token.SignedString(s.Private)
}
func validScopes(scopes []string) bool {
	if len(scopes) < 1 || len(scopes) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if (scope != ReadObjects && scope != WriteOutputs) || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return true
}

func (s CapabilitySigner) Verify(raw string, cert *x509.Certificate, expected TaskIdentity, scope string) (*Claims, error) {
	if len(raw) > 8192 || cert == nil || len(cert.Raw) == 0 || cert.IsCA || !expected.valid() || (scope != ReadObjects && scope != WriteOutputs) || s.Issuer == "" {
		return nil, ErrCapability
	}
	now := s.now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return nil, ErrCapability
	}
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, ErrCapability
		}
		key, ok := s.Public[kid]
		if !ok || len(key) != ed25519.PublicKeySize {
			return nil, ErrCapability
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(s.Issuer), jwt.WithAudience(Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || !token.Valid || claims.TaskIdentity != expected || claims.Subject != expected.WorkerID.String() || len(claims.Audience) != 1 || !validScopes(claims.Scopes) || claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil {
		return nil, ErrCapability
	}
	if claims.NotBefore.Unix() != claims.IssuedAt.Unix() || claims.ExpiresAt.Unix() <= claims.IssuedAt.Unix() || claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > 60*time.Second || claims.ExpiresAt.Time.After(cert.NotAfter) {
		return nil, ErrCapability
	}
	if id, e := uuid.Parse(claims.ID); e != nil || id == uuid.Nil {
		return nil, ErrCapability
	}
	if subtle.ConstantTimeCompare([]byte(claims.CertificateSHA256), []byte(certificateHash(cert))) != 1 {
		return nil, ErrCapability
	}
	for _, allowed := range claims.Scopes {
		if allowed == scope {
			return claims, nil
		}
	}
	return nil, ErrCapability
}

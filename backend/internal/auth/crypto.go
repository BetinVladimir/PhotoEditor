package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

var ErrInvalid = errors.New("invalid credentials")

// NormalizeEmail implements the product's case-insensitive identity policy.
// Display names, quoted local parts and domain literals are intentionally unsupported.
func NormalizeEmail(raw string) (string, error) {
	s := norm.NFC.String(strings.TrimSpace(raw))
	if strings.Count(s, "@") != 1 {
		return "", ErrInvalid
	}
	local, domain, _ := strings.Cut(s, "@")
	local = strings.ToLower(local)
	domain, err := idna.Lookup.ToASCII(strings.ToLower(domain))
	if err != nil || len(local) == 0 || len(local) > 64 || len(domain) == 0 || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", ErrInvalid
	}
	for _, r := range local {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("()<>[]:;\\,\"", r) {
			return "", ErrInvalid
		}
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalid
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", ErrInvalid
			}
		}
	}
	s = local + "@" + domain
	if len(s) > 254 {
		return "", ErrInvalid
	}
	return s, nil
}

func NewSecret(random io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(random, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func NewOTP(random io.Reader) (string, error) {
	n, err := rand.Int(random, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
func ValidOTP(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func HashSecret(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

// MAC is domain separated and length prefixed to prevent ambiguous concatenation.
func MAC(key []byte, purpose string, fields ...string) []byte {
	h := hmac.New(sha256.New, key)
	for _, s := range append([]string{purpose}, fields...) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	return h.Sum(nil)
}
func Equal(a, b []byte) bool { return hmac.Equal(a, b) }

type KeyRing struct {
	Active string
	Keys   map[string][]byte
	Random io.Reader
}

func (k KeyRing) Seal(plain, aad []byte) (string, []byte, error) {
	key, ok := k.Keys[k.Active]
	if !ok || len(key) != 32 {
		return "", nil, errors.New("active AES-256 key unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	random := k.Random
	if random == nil {
		random = rand.Reader
	}
	if _, err = io.ReadFull(random, nonce); err != nil {
		return "", nil, err
	}
	return k.Active, gcm.Seal(nonce, nonce, plain, aad), nil
}
func (k KeyRing) Open(kid string, sealed, aad []byte) ([]byte, error) {
	key, ok := k.Keys[kid]
	if !ok || len(key) != 32 {
		return nil, ErrInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return nil, ErrInvalid
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrInvalid
	}
	return plain, nil
}

type AccessClaims struct {
	SessionID string `json:"sid"`
	Epoch     int64  `json:"ae"`
	jwt.RegisteredClaims
}
type TokenSigner struct {
	Active           string
	Private          ed25519.PrivateKey
	Public           map[string]ed25519.PublicKey
	Issuer, Audience string
	Now              func() time.Time
}

func (s TokenSigner) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s TokenSigner) Issue(user, session uuid.UUID, epoch int64) (string, error) {
	if len(s.Private) != ed25519.PrivateKeySize || s.Active == "" || s.Issuer == "" || s.Audience == "" {
		return "", errors.New("JWT signer unconfigured")
	}
	now := s.now()
	c := AccessClaims{SessionID: session.String(), Epoch: epoch, RegisteredClaims: jwt.RegisteredClaims{Issuer: s.Issuer, Subject: user.String(), Audience: jwt.ClaimStrings{s.Audience}, ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: uuid.NewString()}}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	t.Header["kid"] = s.Active
	return t.SignedString(s.Private)
}
func (s TokenSigner) Verify(raw string) (*AccessClaims, error) {
	if len(raw) > 8192 {
		return nil, ErrInvalid
	}
	c := new(AccessClaims)
	t, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok {
			return nil, ErrInvalid
		}
		key, ok := s.Public[kid]
		if !ok || len(key) != ed25519.PublicKeySize {
			return nil, ErrInvalid
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(s.Issuer), jwt.WithAudience(s.Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil || !t.Valid || c.NotBefore == nil || c.IssuedAt == nil || c.ExpiresAt.Time.Sub(c.IssuedAt.Time) > 5*time.Minute {
		return nil, ErrInvalid
	}
	if _, err = uuid.Parse(c.Subject); err != nil {
		return nil, ErrInvalid
	}
	if _, err = uuid.Parse(c.SessionID); err != nil {
		return nil, ErrInvalid
	}
	return c, nil
}

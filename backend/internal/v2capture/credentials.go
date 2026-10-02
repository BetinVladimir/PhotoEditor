package v2capture

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

var ErrCredential = errors.New("invalid capture credential")

type Credentials struct {
	Signer auth.TokenSigner
	Pepper []byte
	Origin string
	Now    func() time.Time
	Random io.Reader
}

type DeviceClaims struct {
	TenantID  uuid.UUID `json:"tid"`
	ProjectID uuid.UUID `json:"pid"`
	Epoch     int64     `json:"ce"`
	jwt.RegisteredClaims
}

func (c Credentials) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c Credentials) random() io.Reader {
	if c.Random != nil {
		return c.Random
	}
	return rand.Reader
}

func (c Credentials) challengeKeys() (auth.KeyRing, error) {
	if len(c.Pepper) < 32 {
		return auth.KeyRing{}, ErrCredential
	}
	return auth.KeyRing{Active: "capture-v1", Keys: map[string][]byte{
		"capture-v1": auth.MAC(c.Pepper, "capture-challenge-aead-v1")}, Random: c.random()}, nil
}

func challengeAAD(tenantID, deviceID, challengeID uuid.UUID, epoch int64) ([]byte, error) {
	return canonical.Marshal(struct {
		TenantID    uuid.UUID `json:"tenant_id"`
		DeviceID    uuid.UUID `json:"device_id"`
		ChallengeID uuid.UUID `json:"challenge_id"`
		Epoch       int64     `json:"epoch"`
	}{tenantID, deviceID, challengeID, epoch})
}

func (c Credentials) NewChallenge(tenantID, deviceID, challengeID uuid.UUID, epoch int64) (nonce string, keyVersion string, ciphertext []byte, hash string, err error) {
	if tenantID == uuid.Nil || deviceID == uuid.Nil || challengeID == uuid.Nil || epoch < 1 {
		err = ErrCredential
		return
	}
	ring, e := c.challengeKeys()
	if e != nil {
		err = e
		return
	}
	bytes := make([]byte, 32)
	if _, e = io.ReadFull(c.random(), bytes); e != nil {
		err = e
		return
	}
	aad, e := challengeAAD(tenantID, deviceID, challengeID, epoch)
	if e != nil {
		err = e
		return
	}
	keyVersion, ciphertext, e = ring.Seal(bytes, aad)
	if e != nil {
		err = e
		return
	}
	digest := sha256.Sum256(bytes)
	nonce = base64.RawURLEncoding.EncodeToString(bytes)
	hash = hex.EncodeToString(digest[:])
	return
}

func (c Credentials) OpenChallenge(tenantID, deviceID, challengeID uuid.UUID, epoch int64, keyVersion string, ciphertext []byte) (string, error) {
	ring, err := c.challengeKeys()
	if err != nil {
		return "", err
	}
	aad, err := challengeAAD(tenantID, deviceID, challengeID, epoch)
	if err != nil {
		return "", err
	}
	plain, err := ring.Open(keyVersion, ciphertext, aad)
	if err != nil || len(plain) != 32 {
		return "", ErrCredential
	}
	return base64.RawURLEncoding.EncodeToString(plain), nil
}

// SignedChallenge is JCS with fixed field names. The private key never leaves
// the studio agent; server verification uses only the registered public key.
func SignedChallenge(deviceID, challengeID uuid.UUID, nonce string, epoch int64, origin string) ([]byte, error) {
	if deviceID == uuid.Nil || challengeID == uuid.Nil || epoch < 1 || origin == "" || len(nonce) != 43 {
		return nil, ErrCredential
	}
	decoded, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != nonce {
		return nil, ErrCredential
	}
	return canonical.Marshal(struct {
		DeviceID     uuid.UUID `json:"device_id"`
		ChallengeID  uuid.UUID `json:"challenge_id"`
		Nonce        string    `json:"nonce"`
		Epoch        int64     `json:"epoch"`
		ServerOrigin string    `json:"server_origin"`
	}{deviceID, challengeID, nonce, epoch, origin})
}

func VerifyChallengeSignature(publicKey, signature string, payload []byte) error {
	if !validPublicKey(publicKey) || len(signature) != 86 {
		return ErrCredential
	}
	key, _ := base64.RawURLEncoding.DecodeString(publicKey)
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(sig) != signature ||
		!ed25519.Verify(key, payload, sig) {
		return ErrCredential
	}
	return nil
}

func (c Credentials) IssueToken(tenantID, projectID, deviceID uuid.UUID, epoch int64) (string, error) {
	if tenantID == uuid.Nil || projectID == uuid.Nil || deviceID == uuid.Nil || epoch < 1 ||
		len(c.Signer.Private) != ed25519.PrivateKeySize || c.Signer.Active == "" || c.Signer.Issuer == "" {
		return "", ErrCredential
	}
	now := c.now()
	claims := DeviceClaims{TenantID: tenantID, ProjectID: projectID, Epoch: epoch,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: c.Signer.Issuer,
			Subject: deviceID.String(), Audience: jwt.ClaimStrings{"capture"},
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)), ID: uuid.NewString()}}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = c.Signer.Active
	return token.SignedString(c.Signer.Private)
}

// VerifyToken checks cryptography and scope only. Every mutation must also
// compare the returned device ID and epoch against the primary DB row.
func (c Credentials) VerifyToken(raw string) (uuid.UUID, *DeviceClaims, error) {
	if len(raw) > 8192 || c.Signer.Issuer == "" {
		return uuid.Nil, nil, ErrCredential
	}
	claims := new(DeviceClaims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, ErrCredential
		}
		key, ok := c.Signer.Public[kid]
		if !ok || len(key) != ed25519.PublicKeySize {
			return nil, ErrCredential
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(c.Signer.Issuer),
		jwt.WithAudience("capture"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
		jwt.WithTimeFunc(c.now))
	if err != nil || !token.Valid || claims.NotBefore == nil || claims.IssuedAt == nil ||
		claims.ExpiresAt == nil || claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > 15*time.Minute ||
		claims.TenantID == uuid.Nil || claims.ProjectID == uuid.Nil || claims.Epoch < 1 {
		return uuid.Nil, nil, ErrCredential
	}
	deviceID, err := uuid.Parse(claims.Subject)
	if err != nil || deviceID == uuid.Nil {
		return uuid.Nil, nil, ErrCredential
	}
	return deviceID, claims, nil
}

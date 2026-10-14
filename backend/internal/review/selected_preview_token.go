package review

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

var ErrSelectedPreviewToken = errors.New("selected preview token invalid")

// SelectedPreviewGrant is a short-lived capability. The serving endpoint must
// re-read the current decision and object catalogue on every request; a valid
// MAC alone never authorizes the bytes after a review changes.
type SelectedPreviewGrant struct {
	Version       int       `json:"version"`
	Tenant        uuid.UUID `json:"tenant"`
	Review        uuid.UUID `json:"review"`
	User          uuid.UUID `json:"user"`
	Session       uuid.UUID `json:"session"`
	Kind          string    `json:"kind"`
	DecisionHash  string    `json:"decision_hash"`
	Route         string    `json:"route"`
	Variant       string    `json:"variant"`
	Role          string    `json:"role"`
	Artifact      uuid.UUID `json:"artifact"`
	SHA256        string    `json:"sha256"`
	ObjectKeySHA  string    `json:"object_key_sha256"`
	ObjectVersion string    `json:"object_version"`
	ExpiresUnix   int64     `json:"expires_unix"`
}

func (g SelectedPreviewGrant) valid(now time.Time) bool {
	if g.Version != 1 || g.Tenant == uuid.Nil || g.Review == uuid.Nil ||
		g.User == uuid.Nil || g.Session == uuid.Nil || g.Artifact == uuid.Nil ||
		(g.Kind != "render" && g.Kind != "restoration") ||
		(g.Route != "base" && g.Route != "candidate") ||
		(g.Variant != "preview1600" && g.Variant != "preview2560") ||
		(g.Role != "manifest" && g.Role != "image") ||
		!objectstore.ValidInterchangeVersionID(g.ObjectVersion) ||
		g.ExpiresUnix <= now.Unix() || g.ExpiresUnix > now.Add(5*time.Minute).Unix() {
		return false
	}
	if _, err := domain.ParseDigest(g.DecisionHash); err != nil {
		return false
	}
	if _, err := domain.ParseDigest(g.ObjectKeySHA); err != nil {
		return false
	}
	_, err := domain.ParseDigest(g.SHA256)
	return err == nil
}

// SealSelectedPreviewGrant returns a URL-safe HMAC token with no storage key or
// direct object-store credential. The key is deployment-owned and must remain
// stable across API replicas for the token lifetime.
func SealSelectedPreviewGrant(key []byte, grant SelectedPreviewGrant, now time.Time) (string, error) {
	if len(key) < 32 || !grant.valid(now) {
		return "", ErrSelectedPreviewToken
	}
	payload, err := json.Marshal(grant)
	if err != nil || len(payload) > 1024 {
		return "", ErrSelectedPreviewToken
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func OpenSelectedPreviewGrant(key []byte, token string, now time.Time) (SelectedPreviewGrant, error) {
	var zero SelectedPreviewGrant
	if len(key) < 32 || len(token) > 1600 || strings.Count(token, ".") != 1 {
		return zero, ErrSelectedPreviewToken
	}
	parts := strings.SplitN(token, ".", 2)
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return zero, ErrSelectedPreviewToken
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return zero, ErrSelectedPreviewToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) > 1024 || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		return zero, ErrSelectedPreviewToken
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var grant SelectedPreviewGrant
	if decoder.Decode(&grant) != nil || decoder.Decode(new(any)) != io.EOF || !grant.valid(now) {
		return zero, ErrSelectedPreviewToken
	}
	return grant, nil
}

package review

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

var ErrPendingPreviewGrant = errors.New("pending preview grant invalid")

type PendingPreviewGrant struct {
	Version       int       `json:"version"`
	Tenant        uuid.UUID `json:"tenant"`
	Review        uuid.UUID `json:"review"`
	User          uuid.UUID `json:"user"`
	Session       uuid.UUID `json:"session"`
	Side          string    `json:"side"`
	Variant       string    `json:"variant"`
	Source        uuid.UUID `json:"source"`
	SourceSHA     string    `json:"source_sha256"`
	Role          string    `json:"role"`
	Artifact      uuid.UUID `json:"artifact"`
	SHA256        string    `json:"sha256"`
	ObjectKeySHA  string    `json:"object_key_sha256"`
	ObjectVersion string    `json:"object_version"`
	ExpiresUnix   int64     `json:"expires_unix"`
}

func (g PendingPreviewGrant) valid(now time.Time) bool {
	if g.Version != 1 || g.Tenant == uuid.Nil || g.Review == uuid.Nil ||
		g.User == uuid.Nil || g.Session == uuid.Nil || g.Source == uuid.Nil || g.Artifact == uuid.Nil ||
		(g.Side != "before" && g.Side != "after") ||
		(g.Variant != "preview1600" && g.Variant != "preview2560") ||
		(g.Role != "manifest" && g.Role != "image") ||
		!objectstore.ValidInterchangeVersionID(g.ObjectVersion) ||
		g.ExpiresUnix <= now.Unix() || g.ExpiresUnix > now.Add(2*time.Minute).Unix() {
		return false
	}
	_, sourceErr := domain.ParseDigest(g.SourceSHA)
	_, artifactErr := domain.ParseDigest(g.SHA256)
	_, keyErr := domain.ParseDigest(g.ObjectKeySHA)
	return sourceErr == nil && artifactErr == nil && keyErr == nil
}

func SealPendingPreviewGrant(key []byte, grant PendingPreviewGrant, now time.Time) (string, error) {
	if len(key) < 32 || !grant.valid(now) {
		return "", ErrPendingPreviewGrant
	}
	data, err := json.Marshal(grant)
	if err != nil || len(data) > 1024 {
		return "", ErrPendingPreviewGrant
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	signature := auth.MAC(key, "pending-review-preview", payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func OpenPendingPreviewGrant(key []byte, token string, now time.Time) (PendingPreviewGrant, error) {
	var zero PendingPreviewGrant
	if len(key) < 32 || len(token) > 1600 || strings.Count(token, ".") != 1 {
		return zero, ErrPendingPreviewGrant
	}
	parts := strings.SplitN(token, ".", 2)
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != 32 || !auth.Equal(signature, auth.MAC(key, "pending-review-preview", parts[0])) {
		return zero, ErrPendingPreviewGrant
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(data) > 1024 || base64.RawURLEncoding.EncodeToString(data) != parts[0] {
		return zero, ErrPendingPreviewGrant
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var grant PendingPreviewGrant
	if decoder.Decode(&grant) != nil || decoder.Decode(new(any)) != io.EOF || !grant.valid(now) {
		return zero, ErrPendingPreviewGrant
	}
	return grant, nil
}

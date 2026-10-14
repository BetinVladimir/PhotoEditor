package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

var ErrOutputGrant = errors.New("run output grant invalid")

// OutputGrant identifies exactly one review-gated immutable object. A valid
// signature is never enough to serve it: each GET repeats the run-output SQL
// authority query and compares every field before opening storage.
type OutputGrant struct {
	Version       int       `json:"version"`
	Tenant        uuid.UUID `json:"tenant"`
	Run           uuid.UUID `json:"run"`
	User          uuid.UUID `json:"user"`
	Session       uuid.UUID `json:"session"`
	Artifact      uuid.UUID `json:"artifact"`
	Role          string    `json:"role"`
	SHA256        string    `json:"sha256"`
	ObjectKeySHA  string    `json:"object_key_sha256"`
	ObjectVersion string    `json:"object_version"`
	MediaType     string    `json:"media_type"`
	Bytes         int64     `json:"bytes"`
	ExpiresUnix   int64     `json:"expires_unix"`
}

func (g OutputGrant) valid(now time.Time) bool {
	if g.Version != 1 || g.Tenant == uuid.Nil || g.Run == uuid.Nil ||
		g.User == uuid.Nil || g.Session == uuid.Nil || g.Artifact == uuid.Nil ||
		g.Role == "" || len(g.Role) > 64 || strings.ContainsAny(g.Role, "\r\n\x00") ||
		g.MediaType == "" || len(g.MediaType) > 128 || strings.ContainsAny(g.MediaType, "\r\n\x00") ||
		g.Bytes < 2 || g.Bytes > 4*1024*1024*1024 ||
		!objectstore.ValidInterchangeVersionID(g.ObjectVersion) ||
		g.ExpiresUnix <= now.Unix() || g.ExpiresUnix > now.Add(10*time.Minute).Unix() {
		return false
	}
	_, err := domain.ParseDigest(g.SHA256)
	_, keyErr := domain.ParseDigest(g.ObjectKeySHA)
	return err == nil && keyErr == nil
}

func outputObjectKeySHA(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func SealOutputGrant(key []byte, grant OutputGrant, now time.Time) (string, error) {
	if len(key) < 32 || !grant.valid(now) {
		return "", ErrOutputGrant
	}
	data, err := json.Marshal(grant)
	if err != nil || len(data) > 1024 {
		return "", ErrOutputGrant
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := auth.MAC(key, "v26-run-output", payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

func OpenOutputGrant(key []byte, token string, now time.Time) (OutputGrant, error) {
	var zero OutputGrant
	if len(key) < 32 || len(token) > 1600 || strings.Count(token, ".") != 1 {
		return zero, ErrOutputGrant
	}
	parts := strings.SplitN(token, ".", 2)
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(mac) != 32 || !auth.Equal(mac, auth.MAC(key, "v26-run-output", parts[0])) {
		return zero, ErrOutputGrant
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(data) > 1024 || base64.RawURLEncoding.EncodeToString(data) != parts[0] {
		return zero, ErrOutputGrant
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var grant OutputGrant
	if decoder.Decode(&grant) != nil || decoder.Decode(new(any)) != io.EOF || !grant.valid(now) {
		return zero, ErrOutputGrant
	}
	return grant, nil
}

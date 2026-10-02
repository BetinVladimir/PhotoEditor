package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/auth"
	"strings"
	"time"
)

type Cursor struct {
	Tenant       string `json:"tenant"`
	Principal    string `json:"principal"`
	FilterHash   string `json:"filter_hash"`
	Sort         string `json:"sort"`
	LastPosition string `json:"last_position"`
	LastID       string `json:"last_id"`
	Revision     string `json:"revision"`
	Expires      int64  `json:"expires"`
}
type Codec struct {
	Key []byte
	Now func() time.Time
	TTL time.Duration
}

func (c Codec) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func (c Codec) Encode(v Cursor) (string, error) {
	if len(c.Key) < 32 {
		return "", errors.New("cursor key unavailable")
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	v.Expires = c.now().Add(ttl).Unix()
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	data := base64.RawURLEncoding.EncodeToString(b)
	mac := auth.MAC(c.Key, "page-cursor", data)
	return data + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}
func (c Codec) Decode(raw string, scope Cursor) (Cursor, error) {
	invalid := errors.New("invalid cursor")
	if len(c.Key) < 32 || len(raw) > 4096 {
		return Cursor{}, invalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return Cursor{}, invalid
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !auth.Equal(mac, auth.MAC(c.Key, "page-cursor", parts[0])) {
		return Cursor{}, invalid
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Cursor{}, invalid
	}
	var v Cursor
	if json.Unmarshal(b, &v) != nil || v.Expires <= c.now().Unix() || v.Tenant != scope.Tenant || v.Principal != scope.Principal || v.FilterHash != scope.FilterHash || v.Sort != scope.Sort {
		return Cursor{}, invalid
	}
	return v, nil
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Revision   string  `json:"revision,omitempty"`
}

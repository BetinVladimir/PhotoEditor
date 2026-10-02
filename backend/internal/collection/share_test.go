package collection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestShareNormalizationAndSecret(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	expires := now.Add(24 * time.Hour)
	limits := ShareLimits{MaxUploadFiles: 10, MaxUploadBytesTotal: 1024, MaxFileBytes: 512, AllowedFormats: []string{"PNG", "jpeg"}}
	presets := []uuid.UUID{uuid.New(), uuid.New()}
	label, normalized, moderation, gotPresets, err := normalizeShare(" Gallery ", SharePermissions{View: true, Download: true}, limits, "owner_review", presets, &expires, now)
	if err != nil || label != "Gallery" || moderation != "owner_review" || strings.Join(normalized.AllowedFormats, ",") != "jpeg,png" || len(gotPresets) != 2 {
		t.Fatalf("normalize: %#v %#v %q %#v %v", label, normalized, moderation, gotPresets, err)
	}
	s := Service{Pepper: []byte(strings.Repeat("p", 32))}
	id := uuid.New()
	if a, b := s.shareSecret(id), s.shareSecret(id); a != b || len(a) < 32 {
		t.Fatalf("secret is not stable and strong: %q %q", a, b)
	}
}

func TestShareValidationAndOptionalExpiry(t *testing.T) {
	now := time.Now().UTC()
	base := ShareLimits{MaxUploadFiles: 1, MaxUploadBytesTotal: 100, MaxFileBytes: 100, AllowedFormats: []string{"jpeg"}}
	if _, _, _, _, err := normalizeShare("", SharePermissions{}, base, "owner_review", nil, nil, now); err == nil {
		t.Fatal("empty permissions accepted")
	}
	if _, _, _, _, err := normalizeShare("", SharePermissions{Download: true}, base, "owner_review", nil, nil, now); err == nil {
		t.Fatal("download without view accepted")
	}
	bad := base
	bad.AllowedFormats = []string{"svg"}
	if _, _, _, _, err := normalizeShare("", SharePermissions{Upload: true}, bad, "owner_review", nil, nil, now); err == nil {
		t.Fatal("unsafe format accepted")
	}
	type request struct {
		Expires OptionalTime `json:"expires_at,omitempty"`
	}
	var missing, null request
	if json.Unmarshal([]byte(`{}`), &missing) != nil || missing.Expires.Set {
		t.Fatal("missing expiry")
	}
	if json.Unmarshal([]byte(`{"expires_at":null}`), &null) != nil || !null.Expires.Set || null.Expires.Value != nil {
		t.Fatal("null expiry")
	}
}

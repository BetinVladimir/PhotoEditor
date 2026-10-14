package httpapi

import (
	"testing"
	"time"
)

func TestReviewMediaBodyRoleAndSize(t *testing.T) {
	for _, tc := range []struct {
		role, mime string
		body       []byte
		valid      bool
	}{
		{"manifest", "application/json", []byte("{}"), true},
		{"image", "image/jpeg", []byte("jpeg"), true},
		{"image", "image/webp", []byte("webp"), true},
		{"manifest", "image/jpeg", []byte("{}"), false},
		{"image", "text/html", []byte("<script>"), false},
		{"image", "image/jpeg", []byte("x"), false},
		{"manifest", "application/json", make([]byte, 1048577), false},
		{"unknown", "image/jpeg", []byte("jpeg"), false},
	} {
		if validReviewMediaBody(tc.role, tc.body, tc.mime) != tc.valid {
			t.Fatal("review media role/size boundary", tc.role, tc.mime, len(tc.body))
		}
	}
}

func TestReviewMediaGrantExpiresAtFinalResponseBoundary(t *testing.T) {
	now := time.Unix(1000, 0)
	if reviewMediaGrantExpired(1001, now) || !reviewMediaGrantExpired(1000, now) || !reviewMediaGrantExpired(999, now) {
		t.Fatal("capability expiry boundary changed")
	}
}

package review

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPendingPreviewGrantBindsSourceAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("k", 32))
	grant := PendingPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(),
		Side: "after", Variant: "preview1600", Source: uuid.New(), SourceSHA: strings.Repeat("a", 64),
		Role: "image", Artifact: uuid.New(), SHA256: strings.Repeat("b", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "v1", ExpiresUnix: now.Add(time.Minute).Unix()}
	token, err := SealPendingPreviewGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenPendingPreviewGrant(key, token, now); err != nil || got != grant {
		t.Fatal("valid pending preview grant lost", got, err)
	}
	parts := strings.Split(token, ".")
	data, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var changed PendingPreviewGrant
	if err := json.Unmarshal(data, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Source = uuid.New()
	changedBytes, _ := json.Marshal(changed)
	for _, bad := range []string{base64.RawURLEncoding.EncodeToString(changedBytes) + "." + parts[1], token + "x", "a.b.c"} {
		if _, err := OpenPendingPreviewGrant(key, bad, now); !errors.Is(err, ErrPendingPreviewGrant) {
			t.Fatal("tampered pending preview grant accepted", err)
		}
	}
	if _, err := OpenPendingPreviewGrant(key, token, now.Add(time.Minute)); !errors.Is(err, ErrPendingPreviewGrant) {
		t.Fatal("expired pending preview grant accepted", err)
	}
	missingKey := grant
	missingKey.ObjectKeySHA = ""
	if _, err := SealPendingPreviewGrant(key, missingKey, now); !errors.Is(err, ErrPendingPreviewGrant) {
		t.Fatal("unbound object key accepted", err)
	}
	grant.Side = "selected"
	if _, err := SealPendingPreviewGrant(key, grant, now); !errors.Is(err, ErrPendingPreviewGrant) {
		t.Fatal("non-pending side accepted", err)
	}
}

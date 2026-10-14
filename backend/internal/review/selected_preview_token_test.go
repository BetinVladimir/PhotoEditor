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

func TestSelectedPreviewGrantSealAndOpen(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("k", 32))
	grant := SelectedPreviewGrant{
		Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(),
		Session: uuid.New(), Kind: "render", DecisionHash: strings.Repeat("a", 64), Route: "candidate",
		Variant: "preview1600", Role: "image", Artifact: uuid.New(),
		SHA256: strings.Repeat("b", 64), ObjectKeySHA: strings.Repeat("c", 64), ObjectVersion: "version-1", ExpiresUnix: now.Add(2 * time.Minute).Unix(),
	}
	token, err := SealSelectedPreviewGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenSelectedPreviewGrant(key, token, now)
	if err != nil || opened != grant {
		t.Fatal("valid selected-preview grant lost", opened, err)
	}
	parts := strings.Split(token, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var changed SelectedPreviewGrant
	if err := json.Unmarshal(payload, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Review = uuid.New()
	changedBytes, _ := json.Marshal(changed)
	tampered := base64.RawURLEncoding.EncodeToString(changedBytes) + "." + parts[1]
	for _, value := range []string{tampered, token + "x", "", "a.b.c"} {
		if _, err := OpenSelectedPreviewGrant(key, value, now); !errors.Is(err, ErrSelectedPreviewToken) {
			t.Fatal("invalid token accepted", err)
		}
	}
	if _, err := OpenSelectedPreviewGrant(key, token, now.Add(2*time.Minute)); !errors.Is(err, ErrSelectedPreviewToken) {
		t.Fatal("expired token accepted", err)
	}
	for _, mutation := range []func(*SelectedPreviewGrant){
		func(g *SelectedPreviewGrant) { g.ObjectKeySHA = "" },
		func(g *SelectedPreviewGrant) { g.ObjectKeySHA = strings.Repeat("A", 64) },
		func(g *SelectedPreviewGrant) { g.Kind = "unknown" },
		func(g *SelectedPreviewGrant) { g.Route = "skip" },
		func(g *SelectedPreviewGrant) { g.Role = "original" },
		func(g *SelectedPreviewGrant) { g.ObjectVersion = "null" },
		func(g *SelectedPreviewGrant) { g.DecisionHash = strings.Repeat("A", 64) },
		func(g *SelectedPreviewGrant) { g.ExpiresUnix = now.Add(6 * time.Minute).Unix() },
	} {
		bad := grant
		mutation(&bad)
		if _, err := SealSelectedPreviewGrant(key, bad, now); !errors.Is(err, ErrSelectedPreviewToken) {
			t.Fatal("invalid grant sealed", bad, err)
		}
	}
}

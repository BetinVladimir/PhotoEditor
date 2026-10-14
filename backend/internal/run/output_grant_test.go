package run

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOutputGrantPinsOneVersionAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("k", 32))
	grant := OutputGrant{Version: 1, Tenant: uuid.New(), Run: uuid.New(), User: uuid.New(), Session: uuid.New(),
		Artifact: uuid.New(), Role: "working_image", SHA256: strings.Repeat("a", 64),
		ObjectKeySHA: outputObjectKeySHA("selected/output"), ObjectVersion: "v1", MediaType: "image/jpeg", Bytes: 1024, ExpiresUnix: now.Add(time.Minute).Unix()}
	token, err := SealOutputGrant(key, grant, now)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenOutputGrant(key, token, now); err != nil || opened != grant {
		t.Fatal("valid grant lost", opened, err)
	}
	parts := strings.Split(token, ".")
	data, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var changed OutputGrant
	if err := json.Unmarshal(data, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Artifact = uuid.New()
	changedData, _ := json.Marshal(changed)
	for _, bad := range []string{base64.RawURLEncoding.EncodeToString(changedData) + "." + parts[1], token + "x", "a.b.c"} {
		if _, err := OpenOutputGrant(key, bad, now); !errors.Is(err, ErrOutputGrant) {
			t.Fatal("modified grant accepted", err)
		}
	}
	if _, err := OpenOutputGrant(key, token, now.Add(time.Minute)); !errors.Is(err, ErrOutputGrant) {
		t.Fatal("expired grant accepted", err)
	}
	if strings.Contains(string(data), "selected/output") || outputObjectKeySHA(strings.Repeat("a", 1024)) == "" {
		t.Fatal("object key exposed or long-key digest failed")
	}
	for _, mutate := range []func(*OutputGrant){
		func(g *OutputGrant) { g.ObjectKeySHA = "" },
		func(g *OutputGrant) { g.ObjectKeySHA = strings.Repeat("A", 64) },
		func(g *OutputGrant) { g.ObjectVersion = "null" },
		func(g *OutputGrant) { g.SHA256 = strings.Repeat("A", 64) },
		func(g *OutputGrant) { g.ExpiresUnix = now.Add(11 * time.Minute).Unix() },
		func(g *OutputGrant) { g.Bytes = 0 },
	} {
		bad := grant
		mutate(&bad)
		if _, err := SealOutputGrant(key, bad, now); !errors.Is(err, ErrOutputGrant) {
			t.Fatal("invalid output grant sealed", err)
		}
	}
}

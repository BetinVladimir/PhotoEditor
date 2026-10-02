package pagination

import (
	"bytes"
	"testing"
	"time"
)

func TestConfiguredCursorTTLAndScope(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	codec := Codec{Key: bytes.Repeat([]byte{0x45}, 32), Now: func() time.Time { return now }, TTL: 15 * time.Minute}
	scope := Cursor{Tenant: "tenant", Principal: "actor", FilterHash: "project",
		Sort: "v2:project_persons:created_at,id", LastPosition: "2026-01-01T00:00:00Z", LastID: "id"}
	raw, err := codec.Encode(scope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(raw, scope)
	if err != nil || decoded.Expires != now.Add(15*time.Minute).Unix() {
		t.Fatalf("TTL: %+v %v", decoded, err)
	}
	other := scope
	other.FilterHash = "other-project"
	if _, err := codec.Decode(raw, other); err == nil {
		t.Fatal("accepted another project scope")
	}
	codec.Now = func() time.Time { return now.Add(15 * time.Minute) }
	if _, err := codec.Decode(raw, scope); err == nil {
		t.Fatal("accepted expired cursor")
	}
}

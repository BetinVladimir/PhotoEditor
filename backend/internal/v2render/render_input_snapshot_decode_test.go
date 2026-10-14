package v2render

import (
	"strings"
	"testing"
)

func TestRenderInputSnapshotRequestStrict(t *testing.T) {
	binding := `{"asset_version_id":"11111111-1111-1111-1111-111111111111","source":{"artifact_id":"22222222-2222-2222-2222-222222222222","sha256":"` + strings.Repeat("a", 64) + `"},"canvas_hash":"` + strings.Repeat("b", 64) + `"}`
	raw := `{"sources":[` + binding + `]}`
	if items, err := DecodeRenderInputSnapshotRequest([]byte(raw)); err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"sources":null}`, `{"sources":[]}`, `{"sources":[` + binding + `,` + binding + `]}`, strings.Replace(raw, `"sources"`, `"Sources"`, 1), strings.Replace(raw, `"sources":`, `"runtime":"cuda","sources":`, 1), strings.Replace(raw, `"sources":`, `"sources":[],"sources":`, 1), raw + `{}`} {
		if _, err := DecodeRenderInputSnapshotRequest([]byte(bad)); err == nil {
			t.Fatal("invalid snapshot intent accepted")
		}
	}
}

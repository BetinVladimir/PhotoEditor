package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"testing"
)

func retouchParamsFixture() RetouchPreviewParams {
	return RetouchPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "retouch_region"}
}

func TestRetouchPreviewParamsCanonicalRoundTrip(t *testing.T) {
	p := retouchParamsFixture()
	raw, digest, err := FreezeRetouchPreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("incorrect pinned SHA")
	}
	parsed, err := ParseRetouchPreviewParams(raw)
	if err != nil || parsed != p {
		t.Fatal("roundtrip failed", err)
	}
	for _, field := range []string{`"model_id":"big_lama"`, `"context_pixels":128`, `"feather_pixels":8`, `"confirmed":true`, `"approval_id":"fake"`} {
		invalid := append(append([]byte{}, raw[:len(raw)-1]...), []byte(","+field+"}")...)
		if _, err := ParseRetouchPreviewParams(invalid); err == nil {
			t.Fatal("accepted foreign field", field)
		}
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, raw...), ' '),
		bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":true`), 1),
		bytes.Replace(raw, []byte(`"kind":"retouch_region"`), []byte(`"kind":"background_cleanup"`), 1),
	} {
		if _, err := ParseRetouchPreviewParams(invalid); err == nil {
			t.Fatal("accepted invalid exact document")
		}
	}
}

func TestRetouchPreviewParamsRejectScopeAndKindChanges(t *testing.T) {
	for _, mutate := range []func(*RetouchPreviewParams){
		func(p *RetouchPreviewParams) { p.Kind = "remove_object" },
		func(p *RetouchPreviewParams) { p.Kind = "background_cleanup" },
		func(p *RetouchPreviewParams) { p.Kind = "local_exposure" },
		func(p *RetouchPreviewParams) { p.TenantID = uuid.Nil },
		func(p *RetouchPreviewParams) { p.TargetRevision = "01" },
		func(p *RetouchPreviewParams) { p.WorkflowRevision = "9223372036854775807" },
		func(p *RetouchPreviewParams) { p.Protection = p.Candidate },
		func(p *RetouchPreviewParams) { p.ModelLockSHA = "missing" },
	} {
		p := retouchParamsFixture()
		mutate(&p)
		if body, digest, err := FreezeRetouchPreviewParams(p); err == nil || body != nil || digest != "" {
			t.Fatal("invalid proposal frozen")
		}
	}
}

func TestRetouchPreviewParamsSharedPythonWireFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/retouch_preview_params.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseRetouchPreviewParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.WorkflowRevision != "9007199254740993" || p.TargetRevision != "9223372036854775806" || p.Kind != "retouch_region" {
		t.Fatal("cross-language scope changed")
	}
	body, _, err := FreezeRetouchPreviewParams(p)
	if err != nil || !bytes.Equal(body, raw) {
		t.Fatal("shared wire bytes changed", err)
	}
}

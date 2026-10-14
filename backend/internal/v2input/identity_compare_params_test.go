package v2input

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func identityCompareFixture() IdentityCompareParams {
	ref := func() Ref { return Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	return IdentityCompareParams{SchemaVersion: 1,
		Before:        SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)},
		After:         SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("c", 64)},
		EffectiveMask: ref(), Protected: ref(), ModelLockSHA: strings.Repeat("d", 64), Calibration: ref(), Consents: ref()}
}

func TestIdentityCompareParamsCanonicalRoundTrip(t *testing.T) {
	p := identityCompareFixture()
	body, digest, err := CompileIdentityCompareParams(p)
	if err != nil || len(digest) != 64 {
		t.Fatal("compile", err)
	}
	decoded, err := DecodeIdentityCompareParams(body)
	if err != nil || decoded != p {
		t.Fatal("round trip", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"before", "after", "effective_mask", "protected", "model_lock_sha", "calibration", "consents"} {
		if len(object[field]) == 0 {
			t.Fatalf("missing %s", field)
		}
	}
}

func TestIdentityCompareParamsRefusesAliasAndAlternateEncoding(t *testing.T) {
	p := identityCompareFixture()
	for name, mutate := range map[string]func(*IdentityCompareParams){
		"same version":            func(p *IdentityCompareParams) { p.After.AssetVersionID = p.Before.AssetVersionID },
		"same source":             func(p *IdentityCompareParams) { p.After.Source = p.Before.Source },
		"mask aliases protection": func(p *IdentityCompareParams) { p.Protected = p.EffectiveMask },
		"missing consent":         func(p *IdentityCompareParams) { p.Consents = Ref{} },
		"invalid lock":            func(p *IdentityCompareParams) { p.ModelLockSHA = "not-a-digest" },
	} {
		changed := p
		mutate(&changed)
		if _, _, err := CompileIdentityCompareParams(changed); err == nil {
			t.Fatal(name)
		}
	}
	body, _, err := CompileIdentityCompareParams(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"whitespace":   append(append([]byte(nil), body...), '\n'),
		"extra field":  bytes.Replace(body, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":1`), 1),
		"null consent": bytes.Replace(body, []byte(`"consents":{`), []byte(`"consents":null,"discarded":{`), 1),
	} {
		if _, err := DecodeIdentityCompareParams(raw); err == nil {
			t.Fatal(name)
		}
	}
}

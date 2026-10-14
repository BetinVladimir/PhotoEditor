package semanticproposal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestNativeSkinResultCatalogRejectsAmbiguousOrForeignOutputs(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	params := ref()
	outputs := []v2input.Ref{ref(), ref(), ref()}
	id := uuid.New()
	paramsBody, _ := json.Marshal(params)
	body, _ := json.Marshal(outputs)
	valid, err := decodeNativeSkinResultCatalog(id, body, paramsBody, "mask.face_skin", params, nil)
	if err != nil || len(valid.Outputs) != 3 {
		t.Fatal("exact skin result decoder", err)
	}
	for _, kind := range []string{"missing", "duplicate", "params_alias", "foreign_params", "unknown", "trailing", "null", "upstream_alias"} {
		candidate := append([]v2input.Ref(nil), outputs...)
		p := append([]byte(nil), paramsBody...)
		var forbidden []v2input.Ref
		switch kind {
		case "missing":
			candidate = candidate[:2]
		case "duplicate":
			candidate[2] = candidate[0]
		case "params_alias":
			candidate[0] = params
		case "foreign_params":
			p, _ = json.Marshal(ref())
		case "upstream_alias":
			forbidden = []v2input.Ref{candidate[1]}
		}
		raw, _ := json.Marshal(candidate)
		switch kind {
		case "unknown":
			raw = []byte(`[{"artifact_id":"` + outputs[0].ArtifactID.String() + `","sha256":"` + outputs[0].SHA256 + `","unexpected":true}]`)
		case "trailing":
			raw = append(raw, []byte(` {}`)...)
		case "null":
			raw = []byte(`null`)
		}
		decoded, err := decodeNativeSkinResultCatalog(id, raw, p, "mask.face_skin", params, forbidden)
		if err == nil || decoded.ResultID != uuid.Nil || decoded.Outputs != nil {
			t.Fatal("skin decoder exposed invalid result", kind, err)
		}
	}
}

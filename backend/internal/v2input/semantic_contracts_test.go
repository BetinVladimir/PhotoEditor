package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSemanticStrictDocumentContracts(t *testing.T) {
	ref := Ref{uuid.New(), strings.Repeat("a", 64)}
	binding := SessionVersionBinding{uuid.New(), ref, strings.Repeat("b", 64)}
	params := SemanticTransferParams{1, ref, binding, binding, strings.Repeat("c", 64), ref}
	raw, _ := canonical.Marshal(params)
	if _, err := DecodeSemanticTransferParams(raw); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(raw), `"source":{`, `"source":{"unexpected":1,`, 1)
	if _, err := DecodeSemanticTransferParams([]byte(bad)); err == nil {
		t.Fatal("unknown nested key")
	}
	bad = strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)
	if _, err := DecodeSemanticTransferParams([]byte(bad)); err == nil {
		t.Fatal("duplicate key")
	}
	zero := 0.0
	intent := SemanticEditIntent{1, "local_exposure", binding, ref, uuid.New(), .8, &zero, true, nil}
	raw, _ = canonical.Marshal(intent)
	if _, err := DecodeSemanticEditIntent(raw); err != nil {
		t.Fatal("zero EV", err)
	}
	intent.EV = nil
	raw, _ = canonical.Marshal(intent)
	if _, err := DecodeSemanticEditIntent(raw); err == nil {
		t.Fatal("missing exposure EV")
	}
	intent.Kind = "remove_object"
	intent.EV = &zero
	raw, _ = canonical.Marshal(intent)
	if _, err := DecodeSemanticEditIntent(raw); err == nil {
		t.Fatal("EV on removal")
	}
	intent.EV = nil
	intent.PreservePersons = false
	raw, _ = canonical.Marshal(intent)
	if _, err := DecodeSemanticEditIntent(raw); err == nil {
		t.Fatal("person protection disabled")
	}
	intent.PreservePersons = true

	params.Intent.SHA256, _ = canonical.Hash(intent)
	_, owned, err := BindSemanticTransferIntent(params, intent)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "safe"
	intent.Prompt = &prompt
	if owned.Prompt != nil {
		t.Fatal("borrowed intent")
	}
	if _, _, err := BindSemanticTransferIntent(params, intent); err == nil {
		t.Fatal("changed intent SHA")
	}
	intent.Prompt = nil
	raw, _ = canonical.Marshal(intent)
	bad = strings.Replace(string(raw), `"strength":0.8`, `"strength":null`, 1)
	if _, err := DecodeSemanticEditIntent([]byte(bad)); err == nil {
		t.Fatal("null strength")
	}
}

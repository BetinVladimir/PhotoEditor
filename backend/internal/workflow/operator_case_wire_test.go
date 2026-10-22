package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestOperatorCaseScopeUsesExplicitPinnedWire(t *testing.T) {
	expected := OperatorCaseSpec{Schema: 1, ID: "two-source", Tenant: uuid.New(), Run: uuid.New(), Actor: uuid.New()}
	raw, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("wire invalid")
	}
	for _, key := range []string{"tenant_id", "run_id", "actor_id"} {
		if len(fields[key]) == 0 {
			t.Fatal("missing scope key", key)
		}
	}
	for _, key := range []string{"Tenant", "Run", "Actor"} {
		if _, ok := fields[key]; ok {
			t.Fatal("ambiguous scope key", key)
		}
	}
	var replay OperatorCaseSpec
	if decodeOperatorClosed(raw, &replay) != nil {
		t.Fatal("closed replay refused")
	}
	if replay.Tenant != expected.Tenant || replay.Run != expected.Run || replay.Actor != expected.Actor {
		t.Fatal("scope changed on replay")
	}
}

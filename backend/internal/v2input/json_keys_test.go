package v2input

import "testing"

func TestExactJSONKeysRejectsAmbiguousKeys(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `{"a":1} {"a":2}`, `null`, `[]`, `{"b":1}`, `{}`} {
		if exactJSONKeys([]byte(raw), "a") {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"a":null}`, `{"\u0061":[1,2]}`, ` { "a" : {"nested":true} } `} {
		if !exactJSONKeys([]byte(raw), "a") {
			t.Fatalf("rejected %s", raw)
		}
	}
}

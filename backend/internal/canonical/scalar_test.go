package canonical

import (
	"bytes"
	"testing"
)

func TestScalarCanonicalRoots(t *testing.T) {
	for _, item := range []struct{ raw, want string }{{`null`, `null`}, {`true`, `true`}, {`false`, `false`}, {` 1 `, `1`}, {`1.0`, `1`}, {`-0.0`, `0`}, {`-0`, `0`}, {`1e30`, `1e+30`}, {`"a\u0062"`, `"ab"`}, {`"lock-sha"`, `"lock-sha"`}} {
		got, err := Bytes([]byte(item.raw))
		if err != nil || !bytes.Equal(got, []byte(item.want)) {
			t.Fatalf("%s => %s %v", item.raw, got, err)
		}
	}
}
func TestScalarRootRetainsValidation(t *testing.T) {
	for _, raw := range []string{`9007199254740992`, `null true`, `"unterminated`, `NaN`, `Infinity`, `1e999`, `"\ud800"`, `"\udc00"`} {
		if _, err := Bytes([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}

func TestScalarSupportPreservesContainerGolden(t *testing.T) {
	raw := []byte(`{"z":null,"a":[true,"a\u0062",-0.0,{"b":2,"a":1}]}`)
	want := []byte(`{"a":[true,"ab",0,{"a":1,"b":2}],"z":null}`)
	got, err := Bytes(raw)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s %v", got, err)
	}
}
func TestScalarRejectsInvalidUTF8(t *testing.T) {
	if _, err := Bytes([]byte{'"', 0xff, '"'}); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}

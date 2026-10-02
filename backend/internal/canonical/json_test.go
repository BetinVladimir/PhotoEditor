package canonical

import (
	"testing"
)

func TestRFC8785(t *testing.T) {
	input := []byte(`{"z":-0.0,"a":[333333333.33333329,1E30,4.50,2e-3,1e-27],"text":"<>&\u2028"}`)
	got, err := Bytes(input)
	want := "{\"a\":[333333333.3333333,1e+30,4.5,0.002,1e-27],\"text\":\"<>&\u2028\",\"z\":0}"
	if err != nil || string(got) != want {
		t.Fatalf("%s %v", got, err)
	}
}
func TestRejectAmbiguous(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":"\ud800"}`, `{"a":"\udc00"}`, `{"n":9007199254740992}`, `{"n":NaN}`, "{\"x\":\"\xff\"}"} {
		if _, err := Bytes([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}

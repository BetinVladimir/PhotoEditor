package v2optical

import (
	"bytes"
	"math"
	"os"
	"testing"
)

func TestNegativeOriginalParams(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-params.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseNegativeParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	body, sha, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) || len(sha) != 64 {
		t.Fatal(err)
	}
	invalid := [][]byte{append(append([]byte{}, raw...), 32), bytes.Replace(raw, []byte(`"exposure_ev":0,`), nil, 1),
		bytes.Replace(raw, []byte(`"exposure_ev":0`), []byte(`"exposure_ev":true`), 1),
		bytes.Replace(raw, []byte(`"film_base_rgb":[0.8,0.5,0.2]`), []byte(`"film_base_rgb":[0.8,0.5,0.2,1]`), 1),
		bytes.Replace(raw, []byte(`"strength":1`), []byte(`"strength":1,"unknown":1`), 1)}
	for _, b := range invalid {
		if _, err := ParseNegativeParams(b); err == nil {
			t.Fatalf("accepted %s", b)
		}
	}
	for _, n := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
		q := p
		q.Strength = n
		if _, _, err := q.Compile(); err == nil {
			t.Fatal(n)
		}
	}
}

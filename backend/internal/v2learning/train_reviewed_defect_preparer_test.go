package v2learning

import "testing"

func TestReviewedDefectStyleGateRequiresExplicitFlag(t *testing.T) {
	for _, check := range []struct {
		body    string
		enabled bool
		valid   bool
	}{
		{`{"defects":{"enabled":false}}`, false, true},
		{`{"defects":{"enabled":true}}`, true, true},
		{`{"defects":{}}`, false, false},
		{`{"defects":{"enabled":"true"}}`, false, false},
		{`{"beauty":{"enabled":true}}`, false, false},
		{`{`, false, false},
	} {
		got, err := reviewedDefectStyleEnabled([]byte(check.body))
		if (err == nil) != check.valid || (err == nil && got != check.enabled) {
			t.Fatalf("style gate mismatch for %s: %v, %v", check.body, got, err)
		}
	}
}

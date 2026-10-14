package v2learning

import "testing"

func TestPortraitDetailStyleAdmission(t *testing.T) {
	for _, tc := range []struct {
		body    string
		enabled bool
		invalid bool
	}{
		{`{"extensions":{"portrait_detail":{"enabled":true,"teeth_strength":0.4,"shine_strength":0.2,"under_eye_strength":0.3,"glasses_policy":"review"}}}`, true, false},
		{`{"extensions":{"portrait_detail":{"enabled":false}}}`, false, false},
		{`{"extensions":{}}`, false, false},
		{`{"extensions":{"portrait_detail":null}}`, false, false},
		{`{"extensions":{"portrait_detail":{"enabled":"yes"}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":null}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":true}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":true,"teeth_strength":null,"shine_strength":0.2,"under_eye_strength":0.3,"glasses_policy":"review"}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":true,"teeth_strength":1.1,"shine_strength":0.2,"under_eye_strength":0.3,"glasses_policy":"review"}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":true,"teeth_strength":0.4,"shine_strength":0.2,"under_eye_strength":0.3,"glasses_policy":"auto"}}}`, false, true},
		{`{"extensions":{"portrait_detail":{"enabled":false,"teeth_strength":0.4}}}`, false, true},
	} {
		got, err := portraitDetailStyleEnabled([]byte(tc.body))
		if got != tc.enabled || (err != nil) != tc.invalid {
			t.Fatalf("unexpected style admission for %s: %v %v", tc.body, got, err)
		}
	}
}

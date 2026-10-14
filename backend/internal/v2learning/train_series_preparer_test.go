package v2learning

import "testing"

func TestTrainingSeriesStyleRequiresBoundedEnabledModule(t *testing.T) {
	for _, test := range []struct {
		name, body string
		enabled    bool
		invalid    bool
	}{
		{"absent", `{}`, false, false},
		{"disabled", `{"extensions":{"series":{"enabled":false}}}`, false, false},
		{"enabled", `{"extensions":{"series":{"enabled":true,"algorithm":"seriesmatch_v1","reference":"median","ev_limit":0.35,"rgb_gain_limits":[0.95,1.05]}}}`, true, false},
		{"missing algorithm", `{"extensions":{"series":{"enabled":true,"reference":"median","ev_limit":0.35,"rgb_gain_limits":[0.95,1.05]}}}`, false, true},
		{"narrower ev", `{"extensions":{"series":{"enabled":true,"algorithm":"seriesmatch_v1","reference":"median","ev_limit":0.2,"rgb_gain_limits":[0.95,1.05]}}}`, false, true},
		{"foreign reference", `{"extensions":{"series":{"enabled":true,"algorithm":"seriesmatch_v1","reference":"first","ev_limit":0.35,"rgb_gain_limits":[0.95,1.05]}}}`, false, true},
		{"wide gain", `{"extensions":{"series":{"enabled":true,"algorithm":"seriesmatch_v1","reference":"median","ev_limit":0.35,"rgb_gain_limits":[0.94,1.05]}}}`, false, true},
		{"extra disabled", `{"extensions":{"series":{"enabled":false,"algorithm":"seriesmatch_v1"}}}`, false, true},
		{"malformed", `{`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled, err := trainingSeriesStyleEnabled([]byte(test.body))
			if enabled != test.enabled || (err != nil) != test.invalid {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
		})
	}
}

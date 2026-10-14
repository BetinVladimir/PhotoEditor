package v2learning

import "testing"

func TestReviewedDustStyleEnabled(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		enabled bool
		invalid bool
	}{
		{name: "review", body: `{"extensions":{"restoration":{"sensor_dust":"review","denoise":"off","upscale":"off"}}}`, enabled: true},
		{name: "off", body: `{"extensions":{"restoration":{"sensor_dust":"off"}}}`},
		{name: "automatic", body: `{"extensions":{"restoration":{"sensor_dust":"conservative_auto"}}}`},
		{name: "absent", body: `{}`},
		{name: "unknown", body: `{"extensions":{"restoration":{"sensor_dust":"unsafe"}}}`, invalid: true},
		{name: "unmodeled denoise", body: `{"extensions":{"restoration":{"sensor_dust":"review","denoise":"model","upscale":"off"}}}`, invalid: true},
		{name: "malformed", body: `{`, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled, err := reviewedDustStyleEnabled([]byte(test.body))
			if enabled != test.enabled || (err != nil) != test.invalid {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
		})
	}
}

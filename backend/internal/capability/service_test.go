package capability

import "testing"

func TestCapabilityStatusSeparatesEvidenceFromSupport(t *testing.T) {
	cases := []struct {
		name, wantStatus, wantReason string
		supported, available       []string
	}{
		{"active qualified worker", "ready", "", []string{"cpu", "cuda"}, []string{"cuda"}},
		{"supported without worker", "unqualified", "no_qualified_worker", []string{"cpu"}, nil},
		{"no backend", "unsupported", "no_supported_backend", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := classifyBackends(tc.supported, tc.available)
			if status != tc.wantStatus {
				t.Fatalf("status=%q, want %q", status, tc.wantStatus)
			}
			if tc.wantReason == "" && reason != nil {
				t.Fatalf("unexpected reason: %s", *reason)
			}
			if tc.wantReason != "" && (reason == nil || *reason != tc.wantReason) {
				t.Fatalf("reason=%v, want %s", reason, tc.wantReason)
			}
		})
	}
}

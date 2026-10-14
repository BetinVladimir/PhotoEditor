package workflow

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPerspectiveLinesRecoveryEnvironmentExplicitPinsAndBounds(t *testing.T) {
	enabled := "PHOTO_SCHEDULER_PERSPECTIVE_LINES_ENABLED"
	digest := "PHOTO_SCHEDULER_PERSPECTIVE_LINES_RUNTIME_SHA"
	timeout := "PHOTO_SCHEDULER_PERSPECTIVE_LINES_TIMEOUT_SECONDS"
	cases := []struct {
		name                   string
		env                    map[string]string
		wantEnabled, wantError bool
	}{
		{"disabled", map[string]string{digest: "bad"}, false, false},
		{"false", map[string]string{enabled: "false"}, false, false},
		{"invalid flag", map[string]string{enabled: "1"}, false, true},
		{"missing digest", map[string]string{enabled: "true"}, false, true},
		{"valid", map[string]string{enabled: "true", digest: strings.Repeat("a", 64)}, true, false},
		{"bounded", map[string]string{enabled: "true", digest: strings.Repeat("a", 64), timeout: "15"}, true, false},
		{"too long", map[string]string{enabled: "true", digest: strings.Repeat("a", 64), timeout: "301"}, false, true},
		{"overflow", map[string]string{enabled: "true", digest: strings.Repeat("a", 64), timeout: "9223372036854775808"}, false, true},
		{"zero", map[string]string{enabled: "true", digest: strings.Repeat("a", 64), timeout: "0"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, on, err := PerspectiveLinesRecoveryFromEnvironment(func(k string) string { return tc.env[k] })
			if on != tc.wantEnabled || (err != nil) != tc.wantError {
				t.Fatal(on, err)
			}
			if on && (cfg.Timeout < time.Second || cfg.Timeout > 5*time.Minute) {
				t.Fatal("unbounded timeout")
			}
			if tc.name == "valid" && cfg.Timeout != 5*time.Minute {
				t.Fatal("wrong default timeout")
			}
			if tc.name == "bounded" && cfg.Timeout != 15*time.Second {
				t.Fatal("timeout lost")
			}
		})
	}
	if _, _, err := PerspectiveLinesRecoveryFromEnvironment(nil); err == nil {
		t.Fatal("missing environment accepted")
	}
}

func TestPerspectiveLinesRecoveryStartupCancellationAndDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPerspectiveLinesRecoveryProcessor(ctx, nil, nil, "", "", PerspectiveLinesRecoveryConfig{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := NewPerspectiveLinesRecoveryProcessor(context.Background(), nil, nil, "photos", strings.Repeat("a", 64), PerspectiveLinesRecoveryConfig{RuntimeLockSHA: strings.Repeat("b", 64), Timeout: time.Minute}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	if err := verifyPerspectiveLinesRecoveryInstallation(ctx, nil); err != context.Canceled {
		t.Fatal(err)
	}
	if err := verifyPerspectiveLinesRecoveryInstallation(context.Background(), nil); err == nil {
		t.Fatal("missing database accepted")
	}
}

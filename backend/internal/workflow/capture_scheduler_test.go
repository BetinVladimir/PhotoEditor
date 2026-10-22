package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type captureAdmissionError struct{ err error }

func (a captureAdmissionError) AdmitCapture(context.Context, v2input.CaptureAssessmentLease) error {
	return a.err
}

func TestCaptureSchedulerOnlyPersistsKnownFailureCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{v2input.ErrInvalid, "CAPTURE_INPUT_INVALID"},
		{v2input.ErrConflict, "CAPTURE_CONTEXT_CONFLICT"},
		{&pgconn.PgError{Code: "23514", Message: "V24_IDENTITY_CONSENT_REVOKED"}, "CONSENT_REVOKED"},
		{&pgconn.PgError{Code: "23514", Message: "V24_CAPTURE_CONTEXT_STALE"}, "CAPTURE_CONTEXT_STALE"},
		{&pgconn.PgError{Code: "23514", Message: "PRIVATE_INTERNAL_DETAIL"}, ""},
		{v2input.ErrCapturePending, ""},
		{v2input.ErrExpired, ""},
	} {
		got := (captureClassifiedAdmission{captureAdmissionError{tc.err}}).AdmitCapture(context.Background(), v2input.CaptureAssessmentLease{})
		var permanent v2input.CapturePermanentFailure
		if tc.code != "" {
			if !errors.As(got, &permanent) || permanent.Code != tc.code {
				t.Fatalf("classification %v: %v", tc.err, got)
			}
		} else if errors.As(got, &permanent) || !errors.Is(got, tc.err) {
			t.Fatalf("transient/private error changed: %v", got)
		}
	}
}

func TestCaptureSchedulerEnvironmentRequiresQualifiedDeploymentInputs(t *testing.T) {
	env := map[string]string{}
	get := func(key string) string { return env[key] }
	if _, enabled, err := CaptureConfigFromEnvironment(get); err != nil || enabled {
		t.Fatal("default unexpectedly enabled capture", err)
	}
	env["PHOTO_CAPTURE_ENABLED"] = "true"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("missing model lock accepted")
	}
	env["PHOTO_CAPTURE_MODEL_LOCK_SHA"] = strings.Repeat("a", 64)
	cfg, enabled, err := CaptureConfigFromEnvironment(get)
	if err != nil || !enabled || cfg.Backend != "cuda" || cfg.VRAMLimit != 10_000_000_000 || cfg.RAMLimit != 16<<30 {
		t.Fatalf("config=%v enabled=%v error=%v", cfg, enabled, err)
	}
	env["PHOTO_CAPTURE_BACKEND"] = "mps"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("Metal capture accepted an implicit fixed VRAM budget")
	}
	env["PHOTO_CAPTURE_VRAM_BYTES"] = "12884901888"
	cfg, enabled, err = CaptureConfigFromEnvironment(get)
	if err != nil || !enabled || cfg.Backend != "mps" || cfg.VRAMLimit != 12884901888 {
		t.Fatalf("Metal config=%v enabled=%v error=%v", cfg, enabled, err)
	}
	env["PHOTO_CAPTURE_VRAM_BYTES"] = "9007199254740992"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("unrepresentable Metal budget accepted")
	}
	env["PHOTO_CAPTURE_VRAM_BYTES"] = "1"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("impossible GPU budget accepted")
	}
	env["PHOTO_CAPTURE_BACKEND"] = "cpu"
	env["PHOTO_CAPTURE_VRAM_BYTES"] = "0"
	if _, enabled, err := CaptureConfigFromEnvironment(get); err != nil || !enabled {
		t.Fatal("CPU configuration rejected", err)
	}
	env["PHOTO_CAPTURE_RESERVED_BYTES"] = "-1"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("negative reservation accepted")
	}
	env["PHOTO_CAPTURE_ENABLED"] = "yes"
	if _, _, err := CaptureConfigFromEnvironment(get); err == nil {
		t.Fatal("ambiguous activation accepted")
	}
}

package v2input

import (
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestIntegrationPythonLensRegistry(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_LENS_REGISTRY")
	if path == "" {
		t.Skip("Python-generated registry fixture required")
	}
	registry, err := LoadLensRegistry(path, os.Getenv("PHOTO_TEST_LENS_REGISTRY_SHA"))
	if err != nil {
		t.Fatal(err)
	}
	if registry.RuntimeLockSHA != os.Getenv("PHOTO_TEST_LENS_RUNTIME_SHA") || len(registry.Pins) != 1 {
		t.Fatal("cross-language runtime hash or pin mismatch")
	}
	raw, err := os.ReadFile(os.Getenv("PHOTO_TEST_LENS_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(os.Getenv("PHOTO_TEST_LENS_PROFILE_ID"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := VerifyQualifiedLensProfile(raw, Ref{id, os.Getenv("PHOTO_TEST_LENS_PROFILE_SHA")}, registry.RuntimeLockSHA, registry.Pins)
	if err != nil {
		t.Fatal("Python profile pin mismatch", err)
	}
	if profile.CameraModel != "Камера α" || profile.LensModel != "Обектив β" {
		t.Fatal("Unicode profile mismatch")
	}
}

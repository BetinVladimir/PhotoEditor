package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestAuthoredGeometryPreparationStartupRequiresActualInstalledCapture(t *testing.T) {
	for _, key := range []string{"PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT_SHA256", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_API_DATABASE_URL"} {
		t.Setenv(key, "")
	}
	p, close, e := buildAuthoredGeometryPreparation(context.Background(), nil, nil, "")
	if e != nil || p != nil || close == nil {
		t.Fatalf("unconfigured startup: %v", e)
	}
	close()
	for _, key := range []string{"PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT_SHA256", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_API_DATABASE_URL"} {
		t.Setenv(key, "explicit-config")
		p, close, e = buildAuthoredGeometryPreparation(context.Background(), nil, nil, "")
		if !errors.Is(e, ErrAuthoredGeometryInstalledCaptureRequired) || p != nil || close != nil {
			t.Fatalf("configured startup must refuse before dependency IO: %v", e)
		}
		t.Setenv(key, "")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = buildAuthoredGeometryPreparation(ctx, nil, nil, ""); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancelled: %v", e)
	}
}

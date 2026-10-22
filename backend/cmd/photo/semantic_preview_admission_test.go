package main

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/semanticreview"
	"testing"
)

func TestPreviewAdmissionStartupDisabledWithoutConfig(t *testing.T) {
	t.Setenv("PHOTO_SEMANTIC_PREVIEW_CONFIG_FILE", "")
	t.Setenv("PHOTO_SEMANTIC_PREVIEW_CONFIG_SHA256", "")
	got, err := buildSemanticPreviewAdmission(nil, nil, "")
	if err != nil || got != nil {
		t.Fatal("disabled configuration required dependencies", err)
	}
	t.Setenv("PHOTO_SEMANTIC_PREVIEW_CONFIG_FILE", "missing")
	if _, err := buildSemanticPreviewAdmission(nil, nil, ""); err == nil {
		t.Fatal("partial configuration accepted")
	}
}

func TestPreviewAdmissionTenantIsolation(t *testing.T) {
	service := &tenantPreviewAdmission{services: map[uuid.UUID]*semanticreview.ExposurePreviewAdmissionService{uuid.New(): {}}}
	got, err := service.AdmitPreview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrUnavailable || got.RunID != uuid.Nil {
		t.Fatal("foreign tenant admitted", err)
	}
}

func TestBackgroundCleanupAdmissionHasNoRemovalFallback(t *testing.T) {
	tenant := uuid.New()
	service := &tenantPreviewAdmission{
		removal: map[uuid.UUID]*semanticreview.RemovalPreviewAdmissionService{tenant: {}},
		resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
			return "background_cleanup", nil
		},
	}
	_, err := service.AdmitPreview(context.Background(), auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrUnavailable {
		t.Fatal("cleanup borrowed removal installation", err)
	}
	service.backgroundCleanup = map[uuid.UUID]*semanticreview.BackgroundCleanupPreviewAdmissionService{tenant: {}}
	// An installed cleanup service must receive the request and reject its invalid identity.
	_, err = service.AdmitPreview(context.Background(), auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrInvalid {
		t.Fatal("cleanup was not dispatched", err)
	}
	_, err = service.AdmitPreview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrUnavailable {
		t.Fatal("foreign tenant reached cleanup", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.AdmitPreview(ctx, auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}

func TestRetouchAdmissionHasNoRemovalFallback(t *testing.T) {
	tenant := uuid.New()
	service := &tenantPreviewAdmission{
		removal: map[uuid.UUID]*semanticreview.RemovalPreviewAdmissionService{tenant: {}},
		resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
			return "retouch_region", nil
		},
	}
	_, err := service.AdmitPreview(context.Background(), auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrUnavailable {
		t.Fatal("cleanup borrowed removal installation", err)
	}
	service.retouch = map[uuid.UUID]*semanticreview.RetouchPreviewAdmissionService{tenant: {}}
	// An installed cleanup service must receive the request and reject its invalid identity.
	_, err = service.AdmitPreview(context.Background(), auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrInvalid {
		t.Fatal("cleanup was not dispatched", err)
	}
	_, err = service.AdmitPreview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != semanticreview.ErrUnavailable {
		t.Fatal("foreign tenant reached cleanup", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.AdmitPreview(ctx, auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
	if err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}

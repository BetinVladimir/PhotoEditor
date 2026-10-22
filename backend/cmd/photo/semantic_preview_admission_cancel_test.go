package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/semanticreview"
	"testing"
)

func TestTenantCleanupAdmissionPreservesResolverCancellation(t *testing.T) {
	for _, resolverErr := range []error{nil, semanticreview.ErrConflict} {
		ctx, cancel := context.WithCancel(context.Background())
		tenant := uuid.New()
		service := &tenantPreviewAdmission{
			backgroundCleanup: map[uuid.UUID]*semanticreview.BackgroundCleanupPreviewAdmissionService{
				tenant: {},
			},
			resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
				cancel()
				// Unknown kind would otherwise replace cancellation with ErrInvalid.
				return "unknown", resolverErr
			},
		}
		_, err := service.AdmitPreview(ctx, auth.Identity{}, tenant, uuid.New(), uuid.New(), semanticreview.PreviewRequest{})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost after resolver IO", err)
		}
	}
}

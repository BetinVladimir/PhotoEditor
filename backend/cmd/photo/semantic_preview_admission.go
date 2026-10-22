package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
)

type tenantPreviewAdmission struct {
	retouch           map[uuid.UUID]*semanticreview.RetouchPreviewAdmissionService
	backgroundCleanup map[uuid.UUID]*semanticreview.BackgroundCleanupPreviewAdmissionService
	services          map[uuid.UUID]*semanticreview.ExposurePreviewAdmissionService
	removal           map[uuid.UUID]*semanticreview.RemovalPreviewAdmissionService
	resolve           semanticreview.PreviewKindResolver
}

func (s *tenantPreviewAdmission) AdmitPreview(ctx context.Context, who auth.Identity, tenant, intent, target uuid.UUID,
	request semanticreview.PreviewRequest) (semanticreview.PreviewAdmission, error) {
	if err := ctx.Err(); err != nil {
		return semanticreview.PreviewAdmission{}, err
	}
	if s == nil || s.resolve == nil || (s.services[tenant] == nil && s.removal[tenant] == nil && s.backgroundCleanup[tenant] == nil && s.retouch[tenant] == nil) {
		return semanticreview.PreviewAdmission{}, semanticreview.ErrUnavailable
	}
	kind, err := s.resolve(ctx, who, tenant, intent, target, request)
	if err := ctx.Err(); err != nil {
		return semanticreview.PreviewAdmission{}, err
	}
	if err != nil {
		return semanticreview.PreviewAdmission{}, err
	}
	switch kind {
	case "local_exposure":
		if s.services[tenant] != nil {
			return s.services[tenant].AdmitPreview(ctx, who, tenant, intent, target, request)
		}
	case "remove_object":
		if s.removal[tenant] != nil {
			return s.removal[tenant].AdmitPreview(ctx, who, tenant, intent, target, request)
		}
	case "retouch_region":
		if s.retouch[tenant] != nil {
			return s.retouch[tenant].AdmitPreview(ctx, who, tenant, intent, target, request)
		}
	case "background_cleanup":
		if s.backgroundCleanup[tenant] != nil {
			return s.backgroundCleanup[tenant].AdmitPreview(ctx, who, tenant, intent, target, request)
		}
	default:
		return semanticreview.PreviewAdmission{}, semanticreview.ErrInvalid
	}
	return semanticreview.PreviewAdmission{}, semanticreview.ErrUnavailable
}

func buildSemanticPreviewAdmission(db *pgxpool.Pool, storage *objectstore.Store, bucket string) (httpapi.SemanticPreviewAdmission, error) {
	path, sha := os.Getenv("PHOTO_SEMANTIC_PREVIEW_CONFIG_FILE"), os.Getenv("PHOTO_SEMANTIC_PREVIEW_CONFIG_SHA256")
	if path == "" && sha == "" {
		return nil, nil
	}
	if path == "" || sha == "" || db == nil || storage == nil {
		return nil, errors.New("semantic preview configuration incomplete")
	}
	catalog, err := readPinnedExposureCatalog(path, sha)
	if err != nil {
		return nil, err
	}
	reader, err := semanticreview.NewPlanObjectReader(storage, bucket)
	if err != nil {
		return nil, err
	}
	resolve, err := semanticreview.SQLPreviewKindResolver(db, reader)
	if err != nil {
		return nil, err
	}
	faceResolver, err := semanticreview.SQLRetouchPreviewFaceResolver(db, reader)
	if err != nil {
		return nil, err
	}
	service := &tenantPreviewAdmission{retouch: map[uuid.UUID]*semanticreview.RetouchPreviewAdmissionService{}, backgroundCleanup: map[uuid.UUID]*semanticreview.BackgroundCleanupPreviewAdmissionService{}, services: map[uuid.UUID]*semanticreview.ExposurePreviewAdmissionService{}, removal: map[uuid.UUID]*semanticreview.RemovalPreviewAdmissionService{}, resolve: resolve}
	for _, entry := range catalog.Tenants {
		if config := entry.RetouchPreview; config != nil {
			service.retouch[entry.TenantID] = &semanticreview.RetouchPreviewAdmissionService{DB: db, Objects: storage, Reader: reader, Bucket: bucket,
				ReviewBudget: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA,
				OwnerCosts: config.OwnerCosts, Limits: config.Limits, Qualification: config.Qualification, Beauty: config.Beauty, ResolveFaces: faceResolver}
		}

		if config := entry.RemovalPreview; config != nil {
			service.removal[entry.TenantID] = &semanticreview.RemovalPreviewAdmissionService{DB: db, Objects: storage, Reader: reader, Bucket: bucket,
				ReviewBudget: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA,
				OwnerCosts: config.OwnerCosts, Limits: config.Limits, Qualification: config.Qualification, MeasuredDeviceBytes: config.MeasuredDeviceBytes,
				ContextPixels: config.ContextPixels, FeatherPixels: config.FeatherPixels}
		}
		if config := entry.BackgroundCleanupPreview; config != nil {
			service.backgroundCleanup[entry.TenantID] = &semanticreview.BackgroundCleanupPreviewAdmissionService{DB: db, Objects: storage, Reader: reader, Bucket: bucket,
				ReviewBudget: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA,
				OwnerCosts: config.OwnerCosts, Limits: config.Limits, Qualification: config.Qualification, MeasuredDeviceBytes: config.MeasuredDeviceBytes,
				ContextPixels: config.ContextPixels, FeatherPixels: config.FeatherPixels}
		}
		service.services[entry.TenantID] = &semanticreview.ExposurePreviewAdmissionService{DB: db, Objects: storage, Reader: reader, Bucket: bucket,
			ReviewBudget: entry.Review.DocumentBudgetBytes, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA,
			OwnerCosts: entry.Execution.OwnerCosts, Limits: entry.Execution.Limits, Qualification: entry.Execution.Qualification}
	}
	return service, nil
}

func (s *tenantPreviewAdmission) SemanticOperationInstalled(tenant uuid.UUID, operation string) bool {
	if s == nil || s.resolve == nil || tenant == uuid.Nil {
		return false
	}
	switch operation {
	case "semantic.preview_local_exposure":
		return s.services[tenant] != nil
	case "semantic.preview_remove_object":
		return s.removal[tenant] != nil
	case "semantic.preview_retouch_region":
		return s.retouch[tenant] != nil
	case "semantic.preview_background_cleanup":
		return s.backgroundCleanup[tenant] != nil
	default:
		return false
	}
}

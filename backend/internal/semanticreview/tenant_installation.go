package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
)

type ExposureTenantPreparation struct {
	TenantID  uuid.UUID                `json:"tenant_id"`
	Review    ReviewPreparationConfig  `json:"review"`
	Execution ExposureDeploymentBudget `json:"execution"`
}

type ExposurePreparationCatalog struct {
	SchemaVersion int                         `json:"schema_version"`
	Tenants       []ExposureTenantPreparation `json:"tenants"`
}

func DecodeExposurePreparationCatalog(raw []byte) (ExposurePreparationCatalog, error) {
	var config ExposurePreparationCatalog
	if len(raw) < 2 || len(raw) > 4<<20 {
		return config, ErrInvalid
	}
	encoded, err := canonical.Bytes(raw)
	if err != nil {
		return config, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if d.Decode(&config) != nil || config.SchemaVersion != 1 || len(config.Tenants) < 1 || len(config.Tenants) > 1000 {
		return ExposurePreparationCatalog{}, ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ExposurePreparationCatalog{}, ErrInvalid
	}
	seen := make(map[uuid.UUID]bool)
	artifactTenants := make(map[uuid.UUID]uuid.UUID)
	for _, entry := range config.Tenants {
		if entry.TenantID == uuid.Nil || seen[entry.TenantID] {
			return ExposurePreparationCatalog{}, ErrInvalid
		}
		seen[entry.TenantID] = true
		review, _ := json.Marshal(entry.Review)
		if _, err = DecodeReviewPreparationConfig(review); err != nil {
			return ExposurePreparationCatalog{}, err
		}
		if _, err = NewExposureBudgetSelection(entry.Execution); err != nil {
			return ExposurePreparationCatalog{}, err
		}
		for _, artifact := range []uuid.UUID{entry.Execution.Qualification.Report.ArtifactID, entry.Execution.Qualification.Calibration.ArtifactID} {
			if owner, found := artifactTenants[artifact]; found && owner != entry.TenantID {
				return ExposurePreparationCatalog{}, ErrInvalid
			}
			artifactTenants[artifact] = entry.TenantID
		}
	}
	return config, nil
}

type TenantExposureApplyService struct {
	services      map[uuid.UUID]*ApplyService
	fallback      *ApplyService
	closeServices map[uuid.UUID]*CloseService
	closeFallback *CloseService
}

// InstallTenantExposureApplyService owns the catalog and shares verified DB
// pools across tenant-specific pins. Unconfigured tenants retain receipt replay
// and cannot accidentally use another tenant's installation qualification.
func InstallTenantExposureApplyService(ctx context.Context, base ExposureApplyInstallation, catalog ExposurePreparationCatalog) (*TenantExposureApplyService, error) {
	raw, err := canonical.Marshal(catalog)
	if err != nil {
		return nil, ErrInvalid
	}
	owned, err := DecodeExposurePreparationCatalog(raw)
	if err != nil {
		return nil, err
	}
	result := &TenantExposureApplyService{services: make(map[uuid.UUID]*ApplyService, len(owned.Tenants)), fallback: &ApplyService{API: base.API}, closeServices: make(map[uuid.UUID]*CloseService, len(owned.Tenants)), closeFallback: &CloseService{API: base.API}}
	for i, entry := range owned.Tenants {
		config := base
		config.ReviewModelSHA = entry.Review.ModelSHA
		config.ReviewDatasetSHA = entry.Review.DatasetSHA
		config.DocumentBudgetBytes = entry.Review.DocumentBudgetBytes
		config.Execution = entry.Execution
		var service *ApplyService
		if i == 0 {
			service, err = InstallExposureApplyService(ctx, config)
		} else {
			service, err = NewExposureApplyService(config)
		}
		if err != nil {
			return nil, err
		}
		read, closeErr := NewPlanObjectReader(config.Objects, config.Bucket)
		if closeErr != nil {
			return nil, closeErr
		}
		prepareClose, closeErr := NewClosePreparation(config.API, config.Objects, read, config.Bucket, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA)
		if closeErr != nil {
			return nil, closeErr
		}
		result.closeServices[entry.TenantID] = &CloseService{API: config.API, Prepare: prepareClose}
		result.services[entry.TenantID] = service
	}
	return result, nil
}

func (s *TenantExposureApplyService) Apply(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, key string, request ApplyDecision) (idempotency.Response, error) {
	if s == nil || s.fallback == nil {
		return idempotency.Response{}, ErrUnavailable
	}
	service := s.services[tenant]
	if service == nil {
		service = s.fallback
	}
	return service.Apply(ctx, who, tenant, intent, key, request)
}

// Close uses only the selected tenant's review pins; unknown organizations
// retain authenticated receipt replay without fresh document preparation.
func (s *TenantExposureApplyService) Close(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, key string, request ReviewClose) (idempotency.Response, error) {
	if s == nil || s.closeFallback == nil {
		return idempotency.Response{}, ErrUnavailable
	}
	service := s.closeServiceForTenant(tenant)
	return service.Close(ctx, who, tenant, intent, key, request)
}

func (s *TenantExposureApplyService) closeServiceForTenant(tenant uuid.UUID) *CloseService {
	if service := s.closeServices[tenant]; service != nil {
		return service
	}
	return s.closeFallback
}

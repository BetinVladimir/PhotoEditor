package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type analysisProjectKey struct {
	Tenant, Project uuid.UUID
	Operation       string
}
type RelightAnalysisProjects struct {
	services map[analysisProjectKey]*RelightAnalysisService
}

func NewRelightAnalysisProjects(services ...*RelightAnalysisService) (*RelightAnalysisProjects, error) {
	if len(services) < 1 || len(services) > 2048 {
		return nil, v2input.ErrInvalid
	}
	result := &RelightAnalysisProjects{services: make(map[analysisProjectKey]*RelightAnalysisService, len(services))}
	for _, service := range services {
		if service == nil {
			return nil, v2input.ErrInvalid
		}
		key := analysisProjectKey{service.tenant, service.project, service.coordinator.Expected.Operation}
		if result.services[key] != nil {
			return nil, v2input.ErrInvalid
		}
		result.services[key] = service
	}
	return result, nil
}
func (s *RelightAnalysisProjects) SubmitProjectAnalysis(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent RelightAnalysisIntent) (RelightAnalysisReceipt, error) {
	if err := ctx.Err(); err != nil {
		return RelightAnalysisReceipt{}, err
	}
	if s == nil {
		return RelightAnalysisReceipt{}, v2qualification.ErrUnqualified
	}
	service := s.services[analysisProjectKey{tenant, project, intent.Operation}]
	if service == nil {
		return RelightAnalysisReceipt{}, v2qualification.ErrUnqualified
	}
	return service.SubmitIntent(ctx, who, key, intent)
}

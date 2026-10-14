package v2draft

import (
	"context"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// The accepted-source, same-person donor and manual-pair catalogs are shared
// read-only evidence. Open-eyes owns its policy/admission path independently.
func (s *OpenEyesSubmissionService) candidateReader() (*GlareDonorSubmissionService, error) {
	if s == nil || s.Scheduler == nil || s.Tenant == uuid.Nil || s.Project == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	return &GlareDonorSubmissionService{scheduler: s.Scheduler,
		tenant: s.Tenant, project: s.Project}, nil
}

func (s *OpenEyesSubmissionService) ListProjectSourceCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareSourceCandidateQuery) (GlareSourceCandidatePage, error) {
	reader, err := s.candidateReader()
	if err != nil {
		return GlareSourceCandidatePage{}, err
	}
	return reader.ListSourceCandidates(ctx, who, tenant, project, query)
}

func (s *OpenEyesSubmissionService) ListProjectCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareDonorCandidateQuery) (GlareDonorCandidatePage, error) {
	reader, err := s.candidateReader()
	if err != nil {
		return GlareDonorCandidatePage{}, err
	}
	return reader.ListCandidates(ctx, who, tenant, project, query)
}

func (s *OpenEyesSubmissionService) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query GlareMaskPairQuery) (GlareMaskPairPage, error) {
	reader, err := s.candidateReader()
	if err != nil {
		return GlareMaskPairPage{}, err
	}
	return reader.ListMaskPairs(ctx, who, tenant, project, query)
}

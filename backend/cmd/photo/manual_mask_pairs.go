package main

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Both restorations use the same original manual-pair catalog. Select the
// installed project service explicitly so reflection does not depend on a
// donor-glare deployment for mask discovery.
type manualMaskPairProjects struct {
	glare      httpapi.GlareMaskPairs
	reflection httpapi.GlareMaskPairs
	local      httpapi.GlareMaskPairs
}

func (s manualMaskPairProjects) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error) {
	key := [2]uuid.UUID{tenant, project}
	if installed, ok := s.reflection.(reflectionProjectServices); ok && installed.projects[key] != nil {
		return installed.ListProjectMaskPairs(ctx, who, tenant, project, query)
	}
	if installed, ok := s.local.(localPortraitProjectPreparers); ok {
		if _, present := installed.projects[key]; present {
			return installed.ListProjectMaskPairs(ctx, who, tenant, project, query)
		}
	}
	if installed, ok := s.glare.(glareDonorProjectServices); ok && installed.projects[key] != nil {
		return installed.ListProjectMaskPairs(ctx, who, tenant, project, query)
	}
	return v2draft.GlareMaskPairPage{}, v2input.ErrInvalid
}

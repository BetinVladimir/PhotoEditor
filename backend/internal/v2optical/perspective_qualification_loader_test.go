package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

// Reuse byte-stream fault injection while retaining a distinct typed catalogue.
type perspectiveEvidenceIO struct{ negativeEvidenceIO }

func (s *perspectiveEvidenceIO) ResolvePerspectiveQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin PerspectiveQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	return s.negativeEvidenceIO.ResolveNegativeQualification(ctx, who, tenant, project, NegativeQualificationPin(pin))
}

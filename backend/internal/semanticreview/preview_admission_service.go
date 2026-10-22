package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// ExposurePreviewAdmissionService owns server-side preparation. Runtime costs,
// limits and qualification are deployment-owned, never accepted from HTTP.
type ExposurePreviewAdmissionService struct {
	DB                               *pgxpool.Pool
	Objects                          artifactjson.Objects
	Reader                           PlanObjectReader
	Bucket                           string
	ReviewBudget                     int64
	ReviewModelSHA, ReviewDatasetSHA string
	OwnerCosts                       ExposureOwnerCosts
	Limits                           ExposureResourceEstimate
	Qualification                    ExposureQualificationPin
}

func (s *ExposurePreviewAdmissionService) AdmitPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (PreviewAdmission, error) {
	empty := PreviewAdmission{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if request.Validate() != nil || tenant == uuid.Nil || intent == uuid.Nil || target == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, ErrInvalid
	}
	if s == nil || s.DB == nil || s.Objects == nil || s.Reader == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 ||
		s.ReviewBudget < 1 || s.ReviewBudget > 64*1048576 || !shaPattern.MatchString(s.ReviewModelSHA) ||
		!shaPattern.MatchString(s.ReviewDatasetSHA) || !validExposureQualification(s.Qualification) {
		return empty, ErrUnavailable
	}
	// Own all installation measurements before asynchronous IO.
	db, objects, reader, bucket := s.DB, s.Objects, s.Reader, s.Bucket
	budget, model, dataset := s.ReviewBudget, s.ReviewModelSHA, s.ReviewDatasetSHA
	owner, limits, pin := s.OwnerCosts, s.Limits, s.Qualification
	scoped, err := PrepareScopedPreviewReview(ctx, db, who, tenant, intent, target, request, reader, budget, model, dataset)
	if err != nil {
		return empty, err
	}
	source, transform, err := LoadPreviewSourceDocuments(ctx, db, who, tenant, intent, scoped, request, reader, owner, limits, pin)
	if err != nil {
		return empty, err
	}
	input := ExposurePreviewPreparation{Tenant: tenant, Intent: intent, Workflow: scoped.Workflow,
		Review: scoped.Review, Target: scoped.Target, Request: request, Source: source}
	// Stable RunID survives uncertain commit outcomes. Callers must preserve it
	// and use inspection/replay rather than assuming a timeout rolled back.
	return AdmitExposurePreviewRun(ctx, db, objects, bucket, who, input, transform)
}

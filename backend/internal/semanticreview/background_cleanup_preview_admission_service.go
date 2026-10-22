package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// BackgroundCleanupPreviewAdmissionService owns server-side preparation. Runtime costs,
// limits and qualification are deployment-owned, never accepted from HTTP.
type BackgroundCleanupPreviewAdmissionService struct {
	DB                               *pgxpool.Pool
	Objects                          artifactjson.Objects
	Reader                           PlanObjectReader
	Bucket                           string
	ReviewBudget                     int64
	ReviewModelSHA, ReviewDatasetSHA string
	OwnerCosts                       ExposureOwnerCosts
	Limits                           ExposureResourceEstimate
	Qualification                    BackgroundCleanupQualificationPin
	MeasuredDeviceBytes              int64
	ContextPixels, FeatherPixels     int
}

func (s *BackgroundCleanupPreviewAdmissionService) AdmitPreview(ctx context.Context, who auth.Identity,
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
		!shaPattern.MatchString(s.ReviewDatasetSHA) || !validBackgroundCleanupQualification(s.Qualification) || s.ContextPixels < 32 || s.ContextPixels > 512 || s.FeatherPixels < 1 || s.FeatherPixels > 64 {
		return empty, ErrUnavailable
	}
	// Direct service construction must satisfy the same installation contract as
	// the tenant catalog before any authority or object IO takes place.
	if err := validateBackgroundCleanupPreviewDeployment(BackgroundCleanupPreviewDeployment{
		Qualification: s.Qualification, OwnerCosts: s.OwnerCosts, Limits: s.Limits,
		MeasuredDeviceBytes: s.MeasuredDeviceBytes, ContextPixels: s.ContextPixels, FeatherPixels: s.FeatherPixels,
	}); err != nil {
		return empty, ErrUnavailable
	}
	// Own all installation measurements before asynchronous IO.
	db, objects, reader, bucket := s.DB, s.Objects, s.Reader, s.Bucket
	budget, model, dataset := s.ReviewBudget, s.ReviewModelSHA, s.ReviewDatasetSHA
	owner, limits, pin := s.OwnerCosts, s.Limits, s.Qualification
	device, contextPixels, featherPixels := s.MeasuredDeviceBytes, s.ContextPixels, s.FeatherPixels
	scoped, err := PrepareScopedPreviewReview(ctx, db, who, tenant, intent, target, request, reader, budget, model, dataset)
	if err != nil {
		return empty, err
	}
	source, transform, err := LoadBackgroundCleanupPreviewSourceDocuments(ctx, db, who, tenant, intent, scoped, request, reader, owner, limits, pin, device, contextPixels, featherPixels)
	if err != nil {
		return empty, err
	}
	input := BackgroundCleanupPreviewPreparation{ContextPixels: contextPixels, FeatherPixels: featherPixels, Tenant: tenant, Intent: intent, Workflow: scoped.Workflow,
		Review: scoped.Review, Target: scoped.Target, Request: request, Source: source}
	// Stable RunID survives uncertain commit outcomes. Callers must preserve it
	// and use inspection/replay rather than assuming a timeout rolled back.
	return AdmitBackgroundCleanupPreviewRun(ctx, db, objects, bucket, who, input, transform)
}

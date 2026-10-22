package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// RemovalPreviewAdmissionService owns server-side preparation. Runtime costs,
// limits and qualification are deployment-owned, never accepted from HTTP.
type RemovalPreviewAdmissionService struct {
	DB                               *pgxpool.Pool
	Objects                          artifactjson.Objects
	Reader                           PlanObjectReader
	Bucket                           string
	ReviewBudget                     int64
	ReviewModelSHA, ReviewDatasetSHA string
	OwnerCosts                       ExposureOwnerCosts
	Limits                           ExposureResourceEstimate
	Qualification                    RemovalQualificationPin
	MeasuredDeviceBytes              int64
	ContextPixels, FeatherPixels     int
}

func (s *RemovalPreviewAdmissionService) AdmitPreview(ctx context.Context, who auth.Identity,
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
		!shaPattern.MatchString(s.ReviewDatasetSHA) || !validRemovalQualification(s.Qualification) || s.ContextPixels < 32 || s.ContextPixels > 512 || s.FeatherPixels < 1 || s.FeatherPixels > 64 {
		return empty, ErrUnavailable
	}
	// Own all installation measurements before asynchronous IO.
	db, objects, reader, bucket := s.DB, s.Objects, s.Reader, s.Bucket
	budget, model, dataset := s.ReviewBudget, s.ReviewModelSHA, s.ReviewDatasetSHA
	owner, limits, pin := s.OwnerCosts, s.Limits, s.Qualification
	device, contextPixels, featherPixels := s.MeasuredDeviceBytes, s.ContextPixels, s.FeatherPixels
	if validateRemovalPreviewDeployment(RemovalPreviewDeployment{
		Qualification: pin, OwnerCosts: owner, Limits: limits,
		MeasuredDeviceBytes: device, ContextPixels: contextPixels, FeatherPixels: featherPixels,
	}) != nil {
		return empty, ErrUnavailable
	}
	scoped, err := PrepareScopedPreviewReview(ctx, db, who, tenant, intent, target, request, reader, budget, model, dataset)
	if err != nil {
		return empty, err
	}
	source, transform, err := LoadRemovalPreviewSourceDocuments(ctx, db, who, tenant, intent, scoped, request, reader, owner, limits, pin, device, contextPixels, featherPixels)
	if err != nil {
		return empty, err
	}
	input := RemovalPreviewPreparation{ContextPixels: contextPixels, FeatherPixels: featherPixels, Tenant: tenant, Intent: intent, Workflow: scoped.Workflow,
		Review: scoped.Review, Target: scoped.Target, Request: request, Source: source}
	// Stable RunID survives uncertain commit outcomes. Callers must preserve it
	// and use inspection/replay rather than assuming a timeout rolled back.
	return AdmitRemovalPreviewRun(ctx, db, objects, bucket, who, input, transform)
}

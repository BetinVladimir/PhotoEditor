package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RetouchPreviewAdmissionService owns server-side preparation. Runtime costs,
// limits and qualification are deployment-owned, never accepted from HTTP.
type RetouchPreviewAdmissionService struct {
	DB                               *pgxpool.Pool
	Objects                          artifactjson.Objects
	Reader                           PlanObjectReader
	Bucket                           string
	ReviewBudget                     int64
	ReviewModelSHA, ReviewDatasetSHA string
	OwnerCosts                       ExposureOwnerCosts
	Limits                           ExposureResourceEstimate
	Qualification                    RetouchQualificationPin
	Beauty                           RetouchPreviewBeauty
	ResolveFaces                     RetouchPreviewFaceResolver
}

func (s *RetouchPreviewAdmissionService) AdmitPreview(ctx context.Context, who auth.Identity,
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
		!shaPattern.MatchString(s.ReviewDatasetSHA) || !validRetouchQualification(s.Qualification) || !validRetouchBeauty(s.Beauty) || s.ResolveFaces == nil || s.Limits.VRAMBytes != 0 {
		return empty, ErrUnavailable
	}
	if _, err := CompileRetouchPreviewResources(1, 1, s.OwnerCosts, s.Limits); err != nil {
		return empty, ErrUnavailable
	}
	// Own all installation measurements before asynchronous IO.
	db, objects, reader, bucket := s.DB, s.Objects, s.Reader, s.Bucket
	budget, model, dataset := s.ReviewBudget, s.ReviewModelSHA, s.ReviewDatasetSHA
	owner, limits, pin := s.OwnerCosts, s.Limits, s.Qualification
	beauty, resolveFaces := s.Beauty, s.ResolveFaces
	scoped, err := PrepareScopedPreviewReview(ctx, db, who, tenant, intent, target, request, reader, budget, model, dataset)
	if err != nil {
		return empty, err
	}
	source, transform, err := LoadRetouchPreviewSourceDocuments(ctx, db, who, tenant, intent, scoped, request, reader, owner, limits, pin)
	if err != nil {
		return empty, err
	}
	params, err := CompileRetouchPreviewParams(tenant, intent, scoped.Workflow, scoped.Review, scoped.Target, request, source.Protection, pin)
	if err != nil {
		return empty, err
	}
	faces, err := resolveFaces(ctx, who, params)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var candidateRaw []byte
	for _, doc := range scoped.Review.documents {
		if doc.Source.Ref == params.Candidate {
			candidateRaw = doc.Body
		}
	}
	candidate, err := v2input.DecodeSemanticCandidate(candidateRaw)
	if err != nil || candidate.Mask == nil {
		return empty, ErrInvalid
	}
	ref, body, err := BuildRetouchPreviewContext(params, *candidate.Mask, source.Documents.Mask, faces, beauty)
	if err != nil {
		return empty, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, target, request); err != nil {
		return empty, err
	}
	input := RetouchPreviewPreparation{ContextRef: ref, ContextBody: body, Tenant: tenant, Intent: intent, Workflow: scoped.Workflow,
		Review: scoped.Review, Target: scoped.Target, Request: request, Source: source}
	// Stable RunID survives uncertain commit outcomes. Callers must preserve it
	// and use inspection/replay rather than assuming a timeout rolled back.
	return AdmitRetouchPreviewRun(ctx, db, objects, bucket, who, input, transform)
}

// RetouchPreviewFaceResolver resolves exact source-bound face/skin documents on
// the server. SQL admission independently verifies their canonical provenance.
type RetouchPreviewFaceResolver func(context.Context, auth.Identity, RetouchPreviewParams) ([]RetouchPreviewFace, error)

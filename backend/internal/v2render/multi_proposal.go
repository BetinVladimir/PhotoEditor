package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type MultiProposalSource interface {
	AdoptedMultiCatalogueSource
	Binding(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (AdoptedMultiBinding, error)
}
type MultiProposalService struct {
	DB            *pgxpool.Pool
	PreviewSigner PerspectivePreviewSigner
	SRGBSHA       string
	Source        MultiProposalSource
	Objects       v2input.SessionAnalysisObjectReader
	Bucket        string
}
type MultiProposal struct {
	SchemaVersion    int                             `json:"schema_version"`
	Tenant           uuid.UUID                       `json:"tenant_id"`
	Project          uuid.UUID                       `json:"project_id"`
	Producer         uuid.UUID                       `json:"producer_task_id"`
	Operation        string                          `json:"operation"`
	ResultID         uuid.UUID                       `json:"result_id"`
	Result           v2input.Ref                     `json:"result"`
	Sources          []v2input.SessionVersionBinding `json:"sources"`
	ReferenceOrdinal int                             `json:"reference_ordinal"`
	WorkingImage     v2input.Ref                     `json:"working_image"`
	ChangedMask      v2input.Ref                     `json:"changed_mask"`
	Report           v2input.Ref                     `json:"report"`
	Registrations    []v2input.Ref                   `json:"registrations"`
	Canvas           review.Canvas                   `json:"canvas"`
	Status           string                          `json:"status"`
	RequiresReview   bool                            `json:"requires_review"`
	Reason           string                          `json:"reason"`
}

// Describe returns a current, content-verified review candidate. It never applies
// the candidate and does not reinterpret the producer's review report as PASS.
func (s MultiProposalService) Describe(ctx context.Context, who auth.Identity, tenant, project, producer uuid.UUID, operation string) (MultiProposal, error) {
	var empty MultiProposal
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.Source == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || producer == uuid.Nil || (operation != "optical.panorama" && operation != "optical.focus_stack") {
		return empty, v2input.ErrInvalid
	}
	binding, err := s.Source.Binding(ctx, who, tenant, project, producer, operation)
	if err != nil {
		return empty, err
	}
	content, err := LoadAdoptedMultiContent(ctx, s.Source, s.Objects, who, tenant, project, producer, operation, binding.Result, s.Bucket)
	if err != nil {
		return empty, err
	}
	checked, err := content.Binding()
	if err != nil {
		return empty, err
	}
	if checked != binding {
		return empty, v2input.ErrExpired
	}
	image, err := content.Image()
	if err != nil {
		return empty, err
	}
	report, err := content.Report()
	if err != nil {
		return empty, err
	}
	frames, reference, _, err := binding.Inputs()
	if err != nil {
		return empty, err
	}
	current, err := s.Source.Binding(ctx, who, tenant, project, producer, operation)
	if err != nil {
		return empty, err
	}
	if current != binding {
		return empty, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	root := content.documents.root
	return MultiProposal{SchemaVersion: 1, Tenant: tenant, Project: project, Producer: producer, Operation: operation, ResultID: binding.ResultID, Result: binding.Result, Sources: frames, ReferenceOrdinal: reference, WorkingImage: root.WorkingImage, ChangedMask: root.ChangedMask, Report: root.Report, Registrations: report.Registrations, Canvas: image.Canvas, Status: "review", RequiresReview: true, Reason: report.Reason}, nil
}

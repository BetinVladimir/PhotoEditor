package review

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

func (s *Service) ReadCandidatePreviewSource(ctx context.Context, who auth.Identity, tid, id uuid.UUID, side string) (ArtifactRef, []byte, error) {
	if s == nil || s.DB == nil || s.Objects == nil || (side != "before" && side != "after") {
		return ArtifactRef{}, nil, domain.NewError("INPUT_INVALID", 422)
	}
	lookup := func() (decisionContext, error) {
		var value decisionContext
		err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
			var err error
			value, err = loadDecisionContext(ctx, tx, tid, id, s.Bucket)
			if err != nil {
				return err
			}
			if !candidatePreviewKind(value.Kind) {
				return domain.NewError("INPUT_INVALID", 422)
			}
			_, err = project.Authorize(ctx, tx, tid, value.ProjectID, who.UserID, role, domain.Editor, false)
			return err
		})
		return value, err
	}
	initial, err := lookup()
	if err != nil {
		return ArtifactRef{}, nil, err
	}
	source, object := initial.Proposal, initial.ProposalObject
	if side == "before" {
		source, object = initial.Image, initial.ImageObject
	}
	body, err := readObject(ctx, s.Objects, object)
	if err != nil {
		return ArtifactRef{}, nil, err
	}
	if _, err = ParseWorkingImage(body); err != nil {
		return ArtifactRef{}, nil, err
	}
	current, err := lookup()
	if err != nil {
		return ArtifactRef{}, nil, err
	}
	if !sameContext(initial, current) {
		return ArtifactRef{}, nil, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, nil, err
	}
	return source, body, nil
}

var _ CandidatePreviewSource = (*Service)(nil)
var _ CandidatePreviewCatalog = SQLCandidatePreviewCatalog{}

func candidatePreviewKind(kind string) bool {
	switch kind {
	case "render.compose", "optical.negative", "portrait.glare", "portrait.open_eyes", "image.reflection":
		return true
	}
	return false
}

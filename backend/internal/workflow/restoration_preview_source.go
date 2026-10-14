package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RestorationPreviewSource is a read observation. Intent, publication and
// graph seal must repeat the SQL authority; this value is never a lease.
type RestorationPreviewSource struct {
	Tenant, Project, Run, Workflow, Producer, Review, ResultID uuid.UUID
	Result, Candidate                                          v2input.Ref
	Attempt                                                    int
	Fence                                                      int64
	Principal                                                  auth.Identity
	Base                                                       v2input.SessionVersionBinding
}

func validateRestorationPreviewSource(value RestorationPreviewSource) error {
	if value.Tenant == uuid.Nil || value.Project == uuid.Nil || value.Run == uuid.Nil ||
		value.Workflow == uuid.Nil || value.Producer == uuid.Nil ||
		value.Review == uuid.Nil || value.ResultID == uuid.Nil ||
		value.Principal.UserID == uuid.Nil || value.Principal.SessionID == uuid.Nil ||
		value.Base.AssetVersionID == uuid.Nil || value.Attempt < 1 || value.Fence < 1 ||
		value.Result.ArtifactID == uuid.Nil || value.Candidate.ArtifactID == uuid.Nil ||
		value.Base.Source.ArtifactID == uuid.Nil ||
		value.Result.ArtifactID == value.Candidate.ArtifactID ||
		value.Result.ArtifactID == value.Base.Source.ArtifactID ||
		value.Candidate.ArtifactID == value.Base.Source.ArtifactID {
		return v2input.ErrInvalid
	}
	for _, digest := range []string{value.Result.SHA256, value.Candidate.SHA256,
		value.Base.Source.SHA256, value.Base.CanvasHash} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return v2input.ErrInvalid
		}
	}
	workflow, err := v2input.WorkflowID(value.Run)
	if err != nil || workflow != value.Workflow {
		return v2input.ErrInvalid
	}
	return nil
}

type SQLRestorationPreviewSource struct{ DB *pgxpool.Pool }

func (s SQLRestorationPreviewSource) Resolve(ctx context.Context, tenant,
	producer uuid.UUID) (RestorationPreviewSource, error) {
	var zero RestorationPreviewSource
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	value := RestorationPreviewSource{Tenant: tenant, Producer: producer}
	err = tx.QueryRow(bounded, "SELECT * FROM photo.v28_restoration_preview_source($1,$2)",
		tenant, producer).Scan(&value.Project, &value.Run, &value.Workflow,
		&value.Review, &value.ResultID, &value.Result.ArtifactID,
		&value.Result.SHA256, &value.Attempt, &value.Fence,
		&value.Principal.UserID, &value.Principal.SessionID,
		&value.Base.AssetVersionID, &value.Base.Source.ArtifactID,
		&value.Base.Source.SHA256, &value.Base.CanvasHash,
		&value.Candidate.ArtifactID, &value.Candidate.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	if err = validateRestorationPreviewSource(value); err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return value, nil
}

package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2resource"
)

type DraftStore interface {
	ImmutableStore
	v2input.SessionAnalysisObjectReader
}

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
	Objects DraftStore
	Bucket  string
}

func currentDraftStream(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenantID, assetID uuid.UUID, role domain.Role) (uuid.UUID, error) {
	var projectID, versionID uuid.UUID
	e := tx.QueryRow(ctx, `SELECT a.project_id,v.id FROM photo.assets a
	 JOIN LATERAL (SELECT id,review_state FROM photo.asset_versions
	  WHERE tenant_id=a.tenant_id AND asset_id=a.id
	  ORDER BY version_no DESC LIMIT 1) v ON true
	 WHERE a.tenant_id=$1 AND a.id=$2 AND a.deleted_at IS NULL
	 AND a.upload_state='verified' AND v.review_state='accepted'`,
		tenantID, assetID).Scan(&projectID, &versionID)
	if errors.Is(e, pgx.ErrNoRows) {
		return uuid.Nil, domain.NewError("NOT_FOUND", 404)
	}
	if e != nil {
		return uuid.Nil, e
	}
	if _, e = project.Authorize(ctx, tx, tenantID, projectID,
		who.UserID, role, domain.Viewer, false); e != nil {
		return uuid.Nil, e
	}
	var streamID uuid.UUID
	e = tx.QueryRow(ctx, `SELECT id FROM photo.v2_document_streams
	 WHERE tenant_id=$1 AND project_id=$2 AND asset_version_id=$3
	 AND stream_type='draft'`, tenantID, projectID, versionID).Scan(&streamID)
	if errors.Is(e, pgx.ErrNoRows) {
		return uuid.Nil, domain.NewError("NOT_FOUND", 404)
	}
	return streamID, e
}

// Get returns the current immutable EditingRecipeV3 draft for the newest
// accepted version of an asset. The stream and artifact are resolved only
// after tenant and project authorization in one read transaction.
func (s Service) Get(ctx context.Context, who auth.Identity, tenantID,
	assetID uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	var out json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			streamID, e := currentDraftStream(ctx, tx, who, tenantID, assetID, role)
			if e != nil {
				return e
			}
			out, e = v2resource.Load(ctx, tx, tenantID, streamID,
				"v2_document_streams")
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return e
		})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "42P01" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return out, err
}

// Binding returns the verified source of the newest accepted asset version.
// Clients use it to construct an initial EditingRecipeV3 without guessing
// artifact identifiers or the canvas hash from legacy editor metadata.
func (s Service) Binding(ctx context.Context, who auth.Identity, tenantID,
	assetID uuid.UUID) (v2input.SessionVersionBinding, error) {
	binding, _, err := (BindingResolver{DB: s.DB, Objects: s.Objects,
		Bucket: s.Bucket}).CurrentScoped(ctx, who, tenantID, assetID)
	return binding, err
}

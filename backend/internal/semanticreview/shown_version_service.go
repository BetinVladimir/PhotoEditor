package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

// ShownVersionService loads exact object bytes before opening the tenant
// mutation transaction. SQL repeats completed-result and editor authority in
// that transaction, so preflight never grants permission to create a version.
type ShownVersionService struct {
	db     *pgxpool.Pool
	read   PlanObjectReader
	bucket string
}
type ShownVersionResult struct {
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	AssetID        uuid.UUID `json:"asset_id"`
}

func NewShownVersionService(db *pgxpool.Pool, objects planObjectStore, bucket string) (*ShownVersionService, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, ErrInvalid
	}
	read, err := NewPlanObjectReader(objects, bucket)
	if err != nil {
		return nil, err
	}
	return &ShownVersionService{db: db, read: read, bucket: bucket}, nil
}
func (s *ShownVersionService) Materialize(ctx context.Context, who auth.Identity, tid, target uuid.UUID) (ShownVersionResult, error) {
	empty := ShownVersionResult{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s == nil || s.db == nil || s.read == nil {
		return empty, ErrUnavailable
	}
	if tid == uuid.Nil || target == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, ErrInvalid
	}
	document, err := ReadShownAppliedDocument(ctx, s.db, who, tid, target, s.bucket, s.read)
	if err != nil {
		return empty, err
	}
	var result ShownVersionResult
	err = tenant.WithTenant(ctx, s.db, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		version, err := MaterializeShownAppliedVersion(ctx, tx, who, tid, target, document.Body)
		if err != nil {
			return err
		}
		result.AssetVersionID = version
		if err := tx.QueryRow(ctx, "SELECT asset_id FROM photo.asset_versions WHERE tenant_id=$1 AND id=$2 AND review_state='accepted'", tid, version).Scan(&result.AssetID); err != nil {
			return err
		}
		if result.AssetID == uuid.Nil {
			return ErrConflict
		}
		return ctx.Err()
	})
	if cancellation := ctx.Err(); cancellation != nil { return empty, cancellation }
	if err != nil {
		return empty, err
	}
	return result, nil
}

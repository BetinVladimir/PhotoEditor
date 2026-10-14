package v2region

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

// SQLRegionPreparation reads exact authorized metadata under transaction-local
// tenant scope. All S3 reads/canonical source/recipe proofs run after commit;
// no pool connection, snapshot transaction or SQL lock spans object IO.
type SQLRegionPreparation struct {
	API     *pgxpool.Pool
	Objects RegionObjectReader
	Bucket  string
}

func (s SQLRegionPreparation) Prepare(ctx context.Context, tenant, asset, actor, session uuid.UUID, req RegionPreviewRequest) (PreparedRegion, error) {
	if err := ctx.Err(); err != nil {
		return PreparedRegion{}, err
	}
	if s.API == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || asset == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return PreparedRegion{}, ErrRegionInvalid
	}
	var source, recipe RegionSourceMetadata
	err := database.TenantTransaction(ctx, s.API, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return ErrRegionInvalid
		}
		var err error
		source, recipe, err = (Service{DB: tx, Objects: s.Objects, Bucket: s.Bucket}).preparationMetadata(ctx, tenant, asset, actor, session, req)
		return err
	})
	if err != nil {
		return PreparedRegion{}, err
	}
	return (Service{Objects: s.Objects, Bucket: s.Bucket}).finishPreparation(ctx, tenant, asset, req, source, recipe)
}

var _ RegionPreparation = SQLRegionPreparation{}

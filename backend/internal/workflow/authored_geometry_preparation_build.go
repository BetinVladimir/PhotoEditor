package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
)

var ErrAuthoredGeometryInstalledCaptureRequired = errors.New("AUTHORED_GEOMETRY_INSTALLED_CHAIN_CAPTURE_REQUIRED")

// The public SealGraphInTransaction call is real, but neither a source body
// hash nor an arbitrary existing signature proves the whole installed public
// seal/execution/adoption chain. Configured startup deliberately refuses until
// the owning genuine-Goose installed-chain capture successor supplies that gate.
// No readiness attestor/default passed report is invented in this component.
func buildAuthoredGeometryPreparation(ctx context.Context, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (*AuthoredGeometryPreparationProcessor, func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if os.Getenv("PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT") == "" && os.Getenv("PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT_SHA256") == "" && os.Getenv("PHOTO_AUTHORED_GEOMETRY_PREPARATION_API_DATABASE_URL") == "" && os.Getenv("PHOTO_AUTHORED_NATIVE_DEPLOYMENT") == "" && os.Getenv("PHOTO_AUTHORED_NATIVE_DEPLOYMENT_SHA256") == "" {
		return nil, func() {}, nil
	}
	return nil, nil, ErrAuthoredGeometryInstalledCaptureRequired
}

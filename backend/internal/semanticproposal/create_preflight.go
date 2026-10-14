package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// Refuse a miswired public service before publishing request documents. Shared
// pools remain caller-owned; live tenant/project/policy authority is checked
// independently during preparation and the final admission transaction.
func validateCreatePreparer(ctx context.Context, p TransferDraftPreparer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Reader.DB == nil || p.Scheduler == nil || p.Reader.DB == p.Scheduler || p.Documents.DB != p.Reader.DB || p.Reader.Objects == nil || p.Documents.Objects == nil || p.TransportDocuments == nil || p.Reader.ProjectID == uuid.Nil || len(p.Reader.Bucket) < 3 || len(p.Reader.Bucket) > 63 || p.Documents.Bucket != p.Reader.Bucket || p.ReservedBytes <= 0 || p.ReservedBytes > 9007199254740991 {
		return v2input.ErrInvalid
	}
	for _, check := range []struct {
		pool *pgxpool.Pool
		role string
	}{{p.Reader.DB, "photo_api"}, {p.Scheduler, "photo_scheduler"}} {
		var role string
		if err := check.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != check.role {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

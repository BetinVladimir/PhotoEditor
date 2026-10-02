package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"time"
)

type IngestBuildLease struct {
	TenantID, RunID, Token uuid.UUID
	Until                  time.Time
}
type SQLIngestLeases struct{ DB artifactjson.RowQuerier }

func (s SQLIngestLeases) Claim(ctx context.Context) (*IngestBuildLease, error) {
	if s.DB == nil {
		return nil, errors.New("ingest database required")
	}
	lease := new(IngestBuildLease)
	err := s.DB.QueryRow(ctx, "SELECT tenant_id,run_id,lease_token,lease_until FROM photo.claim_ingest_build()").Scan(&lease.TenantID, &lease.RunID, &lease.Token, &lease.Until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lease.TenantID == uuid.Nil || lease.RunID == uuid.Nil || lease.Token == uuid.Nil || lease.Until.IsZero() {
		return nil, errors.New("invalid ingest lease")
	}
	return lease, nil
}
func (s SQLIngestLeases) Renew(ctx context.Context, l IngestBuildLease) (bool, error) {
	return s.update(ctx, "SELECT photo.renew_ingest_build($1,$2)", l)
}
func (s SQLIngestLeases) Release(ctx context.Context, l IngestBuildLease) (bool, error) {
	return s.update(ctx, "SELECT photo.release_ingest_build($1,$2)", l)
}
func (s SQLIngestLeases) update(ctx context.Context, query string, l IngestBuildLease) (bool, error) {
	if s.DB == nil || l.RunID == uuid.Nil || l.Token == uuid.Nil {
		return false, errors.New("invalid ingest lease")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, query, l.RunID, l.Token).Scan(&ok)
	return ok, err
}

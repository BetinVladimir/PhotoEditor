package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
)

type SessionAnalysisConsentPinner interface {
	Pin(context.Context, uuid.UUID, uuid.UUID, SessionInputSnapshot) ([]byte, error)
}

type SQLSessionConsentPinner struct {
	DB     *pgxpool.Pool
	Source SQLSessionConsentSource
	Bucket string
}

// Pin recomputes consent evidence from the frozen run, then atomically
// reserves its exact document hash and links every grant epoch to the workflow.
// A concurrent revoke is serialized by the SQL tenant/grant locks.
func (p SQLSessionConsentPinner) Pin(ctx context.Context, tenant, run uuid.UUID,
	snapshot SessionInputSnapshot) ([]byte, error) {
	if p.DB == nil || p.Source.DB == nil || p.Bucket == "" ||
		tenant == uuid.Nil || run == uuid.Nil {
		return nil, ErrInvalid
	}
	body, err := p.Source.Load(ctx, tenant, run, snapshot)
	if err != nil || len(body) < 2 || len(body) > 8388608 {
		return nil, ErrInvalid
	}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return nil, err
	}
	var pinned bool
	if err := p.DB.QueryRow(ctx, `SELECT photo.pin_v22_analysis_consents(
	 $1,$2,$3::jsonb,$4,$5,$6)`, tenant, run, string(body), sha,
		len(body), p.Bucket).Scan(&pinned); err != nil {
		return nil, err
	}
	if !pinned {
		return nil, ErrConflict
	}
	return body, nil
}

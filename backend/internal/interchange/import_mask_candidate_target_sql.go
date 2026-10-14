package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SQLImportMaskCandidateTarget struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Who              auth.Identity
}

func (s SQLImportMaskCandidateTarget) Resolve(ctx context.Context, parsed []byte, entry uuid.UUID) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Workflow == uuid.Nil || s.Who.UserID == uuid.Nil || s.Who.SessionID == uuid.Nil || entry == uuid.Nil || len(parsed) < 2 || len(parsed) > 8<<20 || !canonicalImportDecisionDocument(string(parsed), 8<<20) {
		return uuid.Nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return uuid.Nil, err
	}
	// Keep the owned bytes stable across the SQL call even if the caller reuses
	// its buffer while the request is in flight.
	owned := append([]byte(nil), parsed...)
	var target *uuid.UUID
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.v30_import_mask_candidate_target($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Workflow, string(owned), entry).Scan(&target)
	})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && target == nil {
		return uuid.Nil, artifactjson.ErrStale
	}
	if err != nil {
		return uuid.Nil, err
	}
	if *target == uuid.Nil {
		return uuid.Nil, artifactjson.ErrStale
	}
	return *target, ctx.Err()
}

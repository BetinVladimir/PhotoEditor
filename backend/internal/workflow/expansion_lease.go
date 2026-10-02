package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type DefectExpansionLease struct {
	ExpansionID, Token, TenantID, RunID, ReviewID, ParentTaskID uuid.UUID
	Until                                                       time.Time
	DecisionHash, Route                                         string
	Selection                                                   review.DefectSelection
}
type SQLExpansionLeases struct{ DB artifactjson.RowQuerier }

func (s SQLExpansionLeases) Claim(ctx context.Context) (*DefectExpansionLease, error) {
	if s.DB == nil {
		return nil, errors.New("expansion database required")
	}
	out := new(DefectExpansionLease)
	var raw []byte
	err := s.DB.QueryRow(ctx, "SELECT expansion_id,lease_token,lease_until,tenant_id,run_id,review_id,parent_task_id,decision_hash,route,selection FROM photo.claim_defect_expansion()").Scan(
		&out.ExpansionID, &out.Token, &out.Until, &out.TenantID, &out.RunID, &out.ReviewID, &out.ParentTaskID, &out.DecisionHash, &out.Route, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 1048576 {
		return nil, errors.New("saved selection exceeds limit")
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return nil, errors.New("invalid saved review selection")
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&out.Selection); err != nil {
		return nil, errors.New("invalid saved review selection")
	}
	if out.ExpansionID == uuid.Nil || out.Token == uuid.Nil || out.TenantID == uuid.Nil || out.RunID == uuid.Nil || out.ReviewID == uuid.Nil || out.ParentTaskID == uuid.Nil || out.Until.IsZero() ||
		(out.Route != "repair" && out.Route != "base" && out.Route != "skip") || out.Selection.Version != "defect_review_v1" || out.Selection.Revision < 2 {
		return nil, errors.New("invalid expansion lease")
	}
	if _, err = domain.ParseDigest(out.DecisionHash); err != nil {
		return nil, errors.New("invalid expansion decision hash")
	}
	return out, nil
}
func (s SQLExpansionLeases) Renew(ctx context.Context, lease DefectExpansionLease) (bool, error) {
	return s.update(ctx, "SELECT photo.renew_defect_expansion($1,$2)", lease)
}
func (s SQLExpansionLeases) Release(ctx context.Context, lease DefectExpansionLease) (bool, error) {
	return s.update(ctx, "SELECT photo.release_defect_expansion($1,$2)", lease)
}
func (s SQLExpansionLeases) update(ctx context.Context, sql string, lease DefectExpansionLease) (bool, error) {
	if s.DB == nil || lease.ExpansionID == uuid.Nil || lease.Token == uuid.Nil {
		return false, errors.New("invalid expansion lease")
	}
	var done bool
	err := s.DB.QueryRow(ctx, sql, lease.ExpansionID, lease.Token).Scan(&done)
	return done, err
}

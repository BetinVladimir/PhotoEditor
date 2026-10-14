package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// exportIntentFromClaim derives only the deterministic identity used to decode
// server-frozen bytes. It does not invent a user session or grant authorization.
func exportIntentFromClaim(c ExportAdoptionClaim) (CreateIntent, error) {
	var zero CreateIntent
	if err := c.validate(); err != nil {
		return zero, err
	}
	request := c.Request
	snapshot := *request.SnapshotID
	request.SnapshotID = &snapshot
	raw, err := canonical.Marshal(request)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	return CreateIntent{Request: request, RequestSHA: c.RequestSHA,
		RequestJCS: raw, Identity: CreateIdentity{
			Run: c.Run, Workflow: c.Workflow, Task: c.Task,
			Snapshot: snapshot,
			Params:   uuid.NewSHA1(c.Task, []byte("interchange:params:1")),
		}}, nil
}

// FrozenExportAdoption recovers and recompiles the exact server ledger plan.
// Its SQL source gate and the independent claim checks bracket this read.
func (s SQLExportAdoptionClaimStore) FrozenExportAdoption(ctx context.Context, c ExportAdoptionClaim) (CreateIntent, FrozenExportDocuments, error) {
	var noIntent CreateIntent
	var noFrozen FrozenExportDocuments
	if err := ctx.Err(); err != nil {
		return noIntent, noFrozen, err
	}
	if s.DB == nil {
		return noIntent, noFrozen, v2input.ErrInvalid
	}
	intent, err := exportIntentFromClaim(c)
	if err != nil {
		return noIntent, noFrozen, err
	}
	current, err := s.CurrentExportAdoptionClaim(ctx, c)
	if err != nil {
		return noIntent, noFrozen, err
	}
	if !current {
		return noIntent, noFrozen, v2input.ErrInvalid
	}
	var raw []byte
	err = s.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_frozen($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		c.Tenant, c.Workflow, c.Run, c.Task, c.Result, c.Actor, c.Key, c.RequestSHA, c.Token).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) || len(raw) == 0 {
		return noIntent, noFrozen, v2input.ErrInvalid
	}
	if err != nil {
		return noIntent, noFrozen, err
	}
	frozen, _, err := DecodeFrozenExportDocuments(ctx, intent, raw)
	if err != nil {
		return noIntent, noFrozen, err
	}
	current, err = s.CurrentExportAdoptionClaim(ctx, c)
	if err != nil {
		return noIntent, noFrozen, err
	}
	if !current {
		return noIntent, noFrozen, v2input.ErrInvalid
	}
	return intent, frozen, ctx.Err()
}

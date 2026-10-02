package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
)

// RegisterLensInputs commits the exact task-scoped transport without activating
// the run. This lets restart recovery finish registration while qualification
// and graph activation remain independently gated.
func (a Admission) RegisterLensInputs(ctx context.Context, tenant, run uuid.UUID, prepared PreparedLensTask) error {
	if a.DB == nil || tenant == uuid.Nil || run == uuid.Nil || !validRef(prepared.Params) {
		return ErrInvalid
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return err
	}
	if !validTaskIdentity(workflow, prepared.Graph.ID, prepared.NodeKey) {
		return ErrInvalid
	}
	if len(prepared.NodeKey) < 13 || prepared.NodeKey[:13] != "optical.lens:" {
		return ErrInvalid
	}
	if !validBuilt(prepared.Graph.Built, prepared.Graph.Direct) || prepared.Graph.Open == nil {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {

		manifest, err := json.Marshal(prepared.Graph.Built.Manifest)
		if err != nil {
			return err
		}
		chunks, err := json.Marshal(prepared.Graph.Built.Chunks)
		if err != nil {
			return err
		}
		direct, err := json.Marshal(prepared.Graph.Direct)
		if err != nil {
			return err
		}
		var replay bool
		if err = tx.QueryRow(ctx, `SELECT photo.v25_lens_registered_inputs($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10::jsonb)`,
			tenant, run, prepared.Graph.ID, prepared.Params.ArtifactID, prepared.Params.SHA256, json.RawMessage(manifest), json.RawMessage(chunks),
			prepared.Graph.Built.Count, prepared.Graph.Built.SHA256, json.RawMessage(direct)).Scan(&replay); err != nil {
			return err
		}
		if replay {
			return nil
		}
		return RegisterInTransaction(ctx, tx, tenant, run, prepared.Graph.ID, prepared.Graph.Built, prepared.Graph.Open, prepared.Graph.Direct)
	})
}

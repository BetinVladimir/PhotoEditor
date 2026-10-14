package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Registers immutable transport only. Caller must roll back errors; no object
// publication or queue activation is permitted inside this transaction.
func (s SQLTransferSourceReader) registerDefectPreparationInputsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan defectPreparationDraftPlan, built v2input.Built) error {
	prepared, err := plan.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	if _, err = s.beginDefectPreparationDraftInTransaction(ctx, tx, who, plan); err != nil {
		return fmt.Errorf("defect registration draft authority: %w", err)
	}
	draft, err := plan.frozen.DraftTask()
	if err != nil {
		return err
	}
	request := plan.request
	request.RunID = plan.shell.Run
	state, err := defectPreparationRegistrationState(ctx, tx, request, draft, prepared.Graph)
	if err != nil {
		return err
	}
	if state == "replayed" {
		return nil
	}
	if state != "missing" {
		return fmt.Errorf("defect registration state %s: %w", state, v2input.ErrConflict)
	}
	if err = v2input.RegisterInTransaction(ctx, tx, request.TenantID, request.RunID, draft.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	state, err = defectPreparationRegistrationState(ctx, tx, request, draft, prepared.Graph)
	if err != nil {
		return err
	}
	if state != "replayed" {
		return fmt.Errorf("defect registration post-write state %s: %w", state, v2input.ErrConflict)
	}
	return nil
}

type defectRegistrationProof struct {
	SchemaVersion int             `json:"schema_version"`
	Rows          json.RawMessage `json:"rows"`
	Manifest      json.RawMessage `json:"manifest"`
	Chunks        json.RawMessage `json:"chunks"`
	Direct        json.RawMessage `json:"direct"`
	Count         int             `json:"count"`
	EntriesSHA    string          `json:"entries_sha"`
}

// Owns the complete bounded transport evidence; cursor is always closed. This
// packet is reusable by a composed SQL gate, without reading objects in SQL.
func freezeDefectRegistrationProof(ctx context.Context, task v2input.GraphTask) (defectRegistrationProof, error) {
	var zero defectRegistrationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if task.Open == nil || task.Built.Count < 3 || task.Built.Count > 4097 || len(task.Direct) != 3 {
		return zero, v2input.ErrInvalid
	}
	cursor, err := task.Open(ctx)
	if err != nil {
		return zero, err
	}
	if cursor == nil {
		return zero, v2input.ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	entries := make([]v2input.Entry, 0, task.Built.Count)
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return zero, err
		}
		if !ok {
			break
		}
		if len(entries) >= task.Built.Count {
			return zero, v2input.ErrInvalid
		}
		entries = append(entries, entry)
	}
	if len(entries) != task.Built.Count {
		return zero, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(entries)
	if err != nil || digest != task.Built.SHA256 {
		return zero, v2input.ErrInvalid
	}
	rows, err := json.Marshal(entries)
	if err != nil {
		return zero, err
	}
	manifest, _ := json.Marshal(task.Built.Manifest)
	chunks, _ := json.Marshal(task.Built.Chunks)
	direct, _ := json.Marshal(task.Direct)

	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return defectRegistrationProof{SchemaVersion: 1, Rows: rows, Manifest: manifest, Chunks: chunks, Direct: direct, Count: task.Built.Count, EntriesSHA: task.Built.SHA256}, nil
}

func defectPreparationRegistrationState(ctx context.Context, tx pgx.Tx, request Request, draft v2input.DraftTask, task v2input.GraphTask) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tx == nil {
		return "", v2input.ErrInvalid
	}
	proof, err := freezeDefectRegistrationProof(ctx, task)
	if err != nil {
		return "", err
	}
	var state string
	err = tx.QueryRow(ctx, "SELECT photo.semantic_defect_preparation_registration_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", request.TenantID, request.RunID, task.ID, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, draft.ExecutionProfile, proof.Rows, proof.Manifest, proof.Chunks, proof.Direct, proof.Count, proof.EntriesSHA).Scan(&state)
	return state, err
}

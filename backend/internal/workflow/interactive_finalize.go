package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type InteractiveFinalizer struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (f InteractiveFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("interactive finalizer database required")
	}
	var tenantID, runID, sourceTask, artifactID, token uuid.UUID
	var ordinal int
	var sha string
	e := f.DB.QueryRow(ctx, `SELECT tenant_id,run_id,source_task_id,ordinal,artifact_id,sha256,token FROM photo.claim_generation_preview()`).Scan(&tenantID, &runID, &sourceTask, &ordinal, &artifactID, &sha, &token)
	if e == nil {
		ref := review.ArtifactRef{ID: artifactID, SHA256: sha}
		params := struct {
			Image   review.ArtifactRef `json:"image"`
			Modules map[string]float64 `json:"modules"`
			Scale   string             `json:"scale"`
		}{ref, map[string]float64{"strength": 1}, "proxy"}
		raw, x := canonical.Marshal(params)
		if x != nil {
			return false, x
		}
		hash, x := canonical.Hash(params)
		if x != nil {
			return false, x
		}
		inputs, x := canonical.Marshal([]review.ArtifactRef{ref})
		if x != nil {
			return false, x
		}
		var ok bool
		x = f.DB.QueryRow(ctx, `SELECT photo.register_generation_preview($1,$2,$3,$4,$5,$6)`, sourceTask, token, uuid.New(), json.RawMessage(raw), hash, json.RawMessage(inputs)).Scan(&ok)
		if x != nil || !ok {
			if x == nil {
				x = errors.New("generation preview registration rejected")
			}
			return false, x
		}
		return true, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return false, e
	}
	var ok bool
	e = f.DB.QueryRow(ctx, `SELECT photo.finalize_interactive_run()`).Scan(&ok)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, e
}

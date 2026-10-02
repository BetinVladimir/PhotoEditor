package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type SQLOriginalCompositionTaskStage struct{ DB *pgxpool.Pool }

// Stage persists a blocked task that exactly matches the saved original plan.
// It repeats live scope under the run lock and never queues or seals a graph.
func (s SQLOriginalCompositionTaskStage) Stage(ctx context.Context, tenant, run, workflow uuid.UUID, ordinal int, draft v2input.DraftTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(run)
	if err != nil || expectedWorkflow != workflow {
		return v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil || task != draft.ID || draft.NodeKey != "layer.compose:"+strconv.Itoa(ordinal) || draft.Operation != "layer.compose" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" {
		return v2input.ErrInvalid
	}
	if !validLayerArtifactRef(v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}) || !validLayerArtifactRef(v2input.Ref{ArtifactID: task, SHA256: draft.InputDigest}) || len(draft.ExecutionProfile) < 2 || len(draft.ExecutionProfile) > 65536 {
		return v2input.ErrInvalid
	}
	profile, err := canonical.Bytes(draft.ExecutionProfile)
	if err != nil || !bytes.Equal(profile, draft.ExecutionProfile) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v25_original_composition_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tenant, run, task, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, profile).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// StageUserMasked repeats complete user-mask authority in the staging transaction.
// The task remains blocked; proposal/closure admission and graph seal are separate.
func (s SQLOriginalCompositionTaskStage) StageUserMasked(ctx context.Context, tenant, run, workflow uuid.UUID, ordinal int, draft v2input.DraftTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(run)
	if err != nil || expectedWorkflow != workflow {
		return v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil || task != draft.ID || draft.NodeKey != "layer.compose:"+strconv.Itoa(ordinal) || draft.Operation != "layer.compose" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" {
		return v2input.ErrInvalid
	}
	if !validLayerArtifactRef(v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}) || !validLayerArtifactRef(v2input.Ref{ArtifactID: task, SHA256: draft.InputDigest}) || len(draft.ExecutionProfile) < 2 || len(draft.ExecutionProfile) > 65536 {
		return v2input.ErrInvalid
	}
	profile, err := canonical.Bytes(draft.ExecutionProfile)
	if err != nil || !bytes.Equal(profile, draft.ExecutionProfile) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v25_user_masked_composition_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tenant, run, task, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, profile).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// StageLensUserMasked additionally repeats exact adopted lens proposal lineage.
// It leaves the task blocked pending complete input registration and qualification.
func (s SQLOriginalCompositionTaskStage) StageLensUserMasked(ctx context.Context, tenant, run, workflow uuid.UUID, ordinal int, draft v2input.DraftTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(run)
	if err != nil || expectedWorkflow != workflow {
		return v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil || task != draft.ID || draft.NodeKey != "layer.compose:"+strconv.Itoa(ordinal) || draft.Operation != "layer.compose" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" {
		return v2input.ErrInvalid
	}
	if !validLayerArtifactRef(v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}) || !validLayerArtifactRef(v2input.Ref{ArtifactID: task, SHA256: draft.InputDigest}) || len(draft.ExecutionProfile) < 2 || len(draft.ExecutionProfile) > 65536 {
		return v2input.ErrInvalid
	}
	profile, err := canonical.Bytes(draft.ExecutionProfile)
	if err != nil || !bytes.Equal(profile, draft.ExecutionProfile) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v25_lens_user_masked_composition_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tenant, run, task, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, profile).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

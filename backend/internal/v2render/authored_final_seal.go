package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Seal executes the complete installed public graph-seal chain. It cannot
// activate a private core, relabel an operation, or accept a staged shell as a
// canonical result. Exact sealed retries retain the current protected scope.
func (s SQLAuthoredFinalAdmission) Seal(ctx context.Context, tenant, parent uuid.UUID, ordinal int, plan AuthoredFinalTaskPlan, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Scheduler == nil || tenant == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal > 4999 {
		return v2input.ErrInvalid
	}
	if err := plan.validateFrozen(); err != nil {
		return err
	}
	if graph.ID != plan.Task.ID || plan.Params.Params.TenantID != tenant || plan.Params.Params.ParentRunID != parent || len(plan.Params.Params.PhaseReceipts)+1 != ordinal {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		matchesPlan := func() error {
			var same bool
			err := tx.QueryRow(ctx, `SELECT t.params_artifact_id=$4 AND t.input_digest=$6
 AND r.config_snapshot#>>'{authored_final,params,sha256}'=$5
 AND r.config_snapshot#>>'{authored_final,params_jcs}'=$7
 AND coalesce(photo.authored_native_replay_consumer_transport_current($1,$3)->'final_profile',
 r.config_snapshot#>'{authored_final,profile}')=$8::jsonb
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=$1 AND r.id=$2 AND t.id=$3 FOR SHARE OF t,r`,
				tenant, plan.Run, plan.Task.ID, plan.ParamsRef.ArtifactID,
				plan.ParamsRef.SHA256, plan.Task.InputDigest, string(plan.Params.JSON), plan.Task.ExecutionProfile).Scan(&same)
			if err != nil {
				return err
			}
			if !same {
				return v2input.ErrExpired
			}
			return ctx.Err()
		}
		var current bool
		if err := tx.QueryRow(ctx, `SELECT photo.authored_render_final_sealed_current($1,$2,$3,$4)`, tenant, parent, ordinal, plan.Task.ID).Scan(&current); err != nil {
			return err
		}
		if current {
			return matchesPlan()
		}
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT photo.lock_authored_render_final_seal_scope($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tenant, parent, ordinal, plan.Task.ID, plan.ParamsRef.ArtifactID, plan.ParamsRef.SHA256, string(plan.Params.JSON), plan.Task.InputDigest, plan.Task.ExecutionProfile).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return v2input.ErrExpired
		}
		if err := v2input.SealGraphInTransaction(ctx, tx, tenant, plan.Run, []v2input.GraphTask{graph}); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT photo.authored_render_final_sealed_current($1,$2,$3,$4)`, tenant, parent, ordinal, plan.Task.ID).Scan(&current); err != nil {
			return err
		}
		if !current {
			return v2input.ErrExpired
		}
		return matchesPlan()
	})
}

// Admit publishes real versioned transport through the scoped SQL repository;
// no caller-supplied success callback or output collection is accepted.
func (s SQLAuthoredFinalAdmission) Admit(ctx context.Context, tenant, parent uuid.UUID, ordinal int, plan AuthoredFinalTaskPlan, bucket string, objects artifactjson.Objects) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if objects == nil || bucket == "" {
		return v2input.ErrInvalid
	}
	if err := s.Stage(ctx, tenant, parent, ordinal, plan); err != nil {
		return err
	}
	graph, err := s.PublishTransport(ctx, tenant, parent, ordinal, plan, bucket, objects)
	if err != nil {
		return err
	}
	return s.Seal(ctx, tenant, parent, ordinal, plan, graph)
}

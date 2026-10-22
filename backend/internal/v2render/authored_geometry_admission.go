package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// AuthoredGeometryScopeWitnesses carries exact immutable documents. SQL verifies
// each hash and original/native/prefix authority; these values grant no authority.
type AuthoredGeometryScopeWitnesses struct {
	Compounds   json.RawMessage `json:"compounds"`
	SourceJCS   string          `json:"source_jcs"`
	ParallelJCS string          `json:"parallel_jcs"`
}
type SQLAuthoredGeometryAdmission struct{ Scheduler *pgxpool.Pool }

func (s SQLAuthoredGeometryAdmission) Stage(ctx context.Context, source AuthoredInstructionSource, plan AuthoredGeometryTaskPlan, witnesses AuthoredGeometryScopeWitnesses) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil || source.Instruction.Kind != "geometry" {
		return v2input.ErrInvalid
	}
	if e := plan.validateFrozen(); e != nil {
		return e
	}
	p := plan.Params.Params
	if p.TenantID != source.Tenant || p.ParentRunID != source.Parent || p.InstructionID != source.Instruction.Identity || p.Source != source.Source || p.ProgramSHA256 != source.ProgramSHA || p.PredecessorSHA256 != source.PredecessorSHA {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, s.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var snapshot uuid.UUID
		if e := tx.QueryRow(ctx, `SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=$1 AND id=$2`, source.Tenant, source.Parent).Scan(&snapshot); e != nil {
			return e
		}
		if snapshot == uuid.Nil {
			return v2input.ErrInvalid
		}
		config, e := canonical.Marshal(map[string]any{"project_id": source.Project, "input_snapshot_id": snapshot, "authored_phase": map[string]any{"parent_run_id": source.Parent, "instruction_id": source.Instruction.Identity, "program_sha256": source.ProgramSHA, "predecessor_sha256": source.PredecessorSHA, "input_source": source.Source}, "authored_geometry": map[string]any{"ordinal": source.Instruction.Ordinal, "task_id": plan.Task.ID, "params": plan.ParamsRef, "params_jcs": string(plan.Params.JSON), "scope_witnesses": witnesses, "input_digest": plan.Task.InputDigest, "profile": json.RawMessage(plan.Task.ExecutionProfile)}})
		if e != nil {
			return e
		}
		var ok bool
		e = tx.QueryRow(ctx, `SELECT photo.begin_authored_render_geometry_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`, source.Tenant, source.Parent, source.Instruction.Ordinal, plan.Task.ID, plan.ParamsRef.ArtifactID, plan.ParamsRef.SHA256, plan.Task.InputDigest, plan.Task.ExecutionProfile, string(config)).Scan(&ok)
		if e != nil {
			return e
		}
		if !ok {
			return v2input.ErrExpired
		}
		return ctx.Err()
	})
}
func (s SQLAuthoredGeometryAdmission) Seal(ctx context.Context, source AuthoredInstructionSource, plan AuthoredGeometryTaskPlan, graph v2input.GraphTask) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil || graph.ID != plan.Task.ID {
		return v2input.ErrInvalid
	}
	if e := plan.validateFrozen(); e != nil {
		return e
	}
	return database.TenantTransaction(ctx, s.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var current bool
		if e := tx.QueryRow(ctx, `SELECT photo.authored_render_geometry_sealed_current($1,$2,$3)`, source.Tenant, source.Parent, plan.Task.ID).Scan(&current); e != nil {
			return e
		}
		if !current {
			var locked bool
			if e := tx.QueryRow(ctx, `SELECT photo.lock_authored_render_geometry_seal_scope($1,$2,$3,$4)`, source.Tenant, source.Parent, source.Instruction.Ordinal, plan.Task.ID).Scan(&locked); e != nil {
				return e
			}
			if !locked {
				return v2input.ErrExpired
			}
			if e := v2input.SealGraphInTransaction(ctx, tx, source.Tenant, plan.Run, []v2input.GraphTask{graph}); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT photo.authored_render_geometry_sealed_current($1,$2,$3)`, source.Tenant, source.Parent, plan.Task.ID).Scan(&current); e != nil {
				return e
			}
			if !current {
				return v2input.ErrExpired
			}
		}
		var same bool
		if e := tx.QueryRow(ctx, `SELECT t.params_artifact_id=$4 AND t.input_digest=$5 AND r.config_snapshot#>>'{authored_geometry,params_jcs}'=$6 AND r.config_snapshot#>>'{authored_geometry,params,sha256}'=$7 AND (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=$8::jsonb FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=$1 AND r.id=$2 AND t.id=$3`, source.Tenant, plan.Run, plan.Task.ID, plan.ParamsRef.ArtifactID, plan.Task.InputDigest, string(plan.Params.JSON), plan.ParamsRef.SHA256, plan.Task.ExecutionProfile).Scan(&same); e != nil {
			return e
		}
		if !same {
			return v2input.ErrExpired
		}
		return ctx.Err()
	})
}
func (s SQLAuthoredGeometryAdmission) Admit(ctx context.Context, source AuthoredInstructionSource, plan AuthoredGeometryTaskPlan, witnesses AuthoredGeometryScopeWitnesses, bucket string, objects artifactjson.Objects) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil || bucket == "" || objects == nil {
		return v2input.ErrInvalid
	}
	if e := plan.validateFrozen(); e != nil {
		return e
	}
	// Sealed retries never restage or republish an immutable child.
	var current bool
	e := database.TenantTransaction(ctx, s.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_render_geometry_sealed_current($1,$2,$3)`, source.Tenant, source.Parent, plan.Task.ID).Scan(&current)
	})
	if e != nil {
		return e
	}
	if current {
		return s.Seal(ctx, source, plan, v2input.GraphTask{ID: plan.Task.ID})
	}
	if e = s.Stage(ctx, source, plan, witnesses); e != nil {
		return e
	}
	publisher := v2input.ScopedPublisher{Tenant: source.Tenant, Run: plan.Run, Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: s.Scheduler, Bucket: bucket}, Objects: objects}}
	graph, e := PublishAuthoredGeometryTransport(ctx, plan, publisher)
	if e != nil {
		return e
	}
	return s.Seal(ctx, source, plan, graph)
}

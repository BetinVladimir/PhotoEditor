package v2render

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Admission never invokes a private graph core. The independently installed
// outer public seal/execution chain remains mandatory even after this scope.
type SQLAuthoredNativeAdmission struct{ Scheduler *pgxpool.Pool }

func (s SQLAuthoredNativeAdmission) Stage(ctx context.Context, m AuthoredNativeMaterialization, p AuthoredNativeTaskPlan) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil {
		return v2input.ErrInvalid
	}
	if e := p.validateFrozen(); e != nil {
		return e
	}
	run, workflow, task, e := authoredNativeIdentities(m)
	if e != nil || run != p.Run || workflow != p.Workflow || task != p.Task.ID {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, s.Scheduler, m.source.Tenant, func(tx pgx.Tx) error {
		var ok bool
		e := tx.QueryRow(ctx, `SELECT photo.begin_authored_native_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`, m.source.Tenant, m.source.Parent, m.source.Instruction.Ordinal, p.Task.ID, p.ParamsRef.ArtifactID, p.ParamsRef.SHA256, p.Task.InputDigest, p.Task.ExecutionProfile, string(p.config)).Scan(&ok)
		if e != nil {
			return e
		}
		if !ok {
			return v2input.ErrExpired
		}
		return ctx.Err()
	})
}
func (s SQLAuthoredNativeAdmission) Seal(ctx context.Context, m AuthoredNativeMaterialization, p AuthoredNativeTaskPlan, g v2input.GraphTask) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil || g.ID != p.Task.ID {
		return v2input.ErrInvalid
	}
	if e := p.validateFrozen(); e != nil {
		return e
	}
	return database.TenantTransaction(ctx, s.Scheduler, m.source.Tenant, func(tx pgx.Tx) error {
		var current bool
		if e := tx.QueryRow(ctx, `SELECT photo.authored_native_sealed_current($1,$2,$3)`, m.source.Tenant, m.source.Parent, p.Task.ID).Scan(&current); e != nil {
			return e
		}
		if !current {
			var locked bool
			if e := tx.QueryRow(ctx, `SELECT photo.lock_authored_native_seal_scope($1,$2,$3)`, m.source.Tenant, m.source.Parent, p.Task.ID).Scan(&locked); e != nil {
				return e
			}
			if !locked {
				return v2input.ErrExpired
			}
			if e := v2input.SealGraphInTransaction(ctx, tx, m.source.Tenant, p.Run, []v2input.GraphTask{g}); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT photo.authored_native_sealed_current($1,$2,$3)`, m.source.Tenant, m.source.Parent, p.Task.ID).Scan(&current); e != nil {
				return e
			}
			if !current {
				return v2input.ErrExpired
			}
		}
		// Immutable retry equality includes the entire descriptor and compute
		// profile, permitting only the real sealed transport selectors.
		var same bool
		if e := tx.QueryRow(ctx, `SELECT r.config_snapshot=$4::jsonb AND t.params_artifact_id=$5 AND t.input_digest=$6 AND (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=$7::jsonb FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2 AND t.id=$3`, m.source.Tenant, p.Run, p.Task.ID, p.config, p.ParamsRef.ArtifactID, p.Task.InputDigest, p.Task.ExecutionProfile).Scan(&same); e != nil {
			return e
		}
		if !same {
			return v2input.ErrConflict
		}
		return ctx.Err()
	})
}
func (s SQLAuthoredNativeAdmission) Admit(ctx context.Context, m AuthoredNativeMaterialization, p AuthoredNativeTaskPlan, bucket string, objects artifactjson.Objects) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.Scheduler == nil || bucket == "" || objects == nil {
		return v2input.ErrInvalid
	}
	if e := p.validateFrozen(); e != nil {
		return e
	}
	var current bool
	e := database.TenantTransaction(ctx, s.Scheduler, m.source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_native_sealed_current($1,$2,$3)`, m.source.Tenant, m.source.Parent, p.Task.ID).Scan(&current)
	})
	if e != nil {
		return e
	}
	if current {
		return s.Seal(ctx, m, p, v2input.GraphTask{ID: p.Task.ID})
	}
	if e = s.Stage(ctx, m, p); e != nil {
		return e
	}
	publisher := v2input.ScopedPublisher{Tenant: m.source.Tenant, Run: p.Run, Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: s.Scheduler, Bucket: bucket}, Objects: objects}}
	graph, e := PublishAuthoredNativeTransport(ctx, p, publisher)
	if e != nil {
		return e
	}
	return s.Seal(ctx, m, p, graph)
}

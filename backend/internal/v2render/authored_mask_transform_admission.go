package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Admission publishes actual immutable Params/closure and calls the existing
// public registered-input seal. It never marks a task succeeded or adopted.
// Installed source/execution/adoption namespace mounting is separately required.
type AuthoredMaskTransformPreparation struct {
	Scheduler *pgxpool.Pool
	Params    AuthoredMaskTransformParamsPublication
}

func (p AuthoredMaskTransformPreparation) Admit(ctx context.Context, source AuthoredInstructionSource, plan AuthoredMaskTransformPlan, previousRootJCS *string, targetJCS string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || p.Params.Objects == nil || p.Params.Bucket == "" {
		return v2input.ErrInvalid
	}
	// Own caller-supplied optional bytes before the first transaction/IO.
	if previousRootJCS != nil {
		copy := *previousRootJCS
		previousRootJCS = &copy
	}
	raw, err := plan.Frozen.ParamsBytes()
	if err != nil {
		return err
	}
	draft, err := plan.Frozen.DraftTask()
	if err != nil {
		return err
	}
	descriptor, err := canonical.Marshal(plan.Descriptor)
	if err != nil {
		return err
	}
	var owned AuthoredMaskTransformDescriptor
	if json.Unmarshal(descriptor, &owned) != nil {
		return v2input.ErrInvalid
	}
	plan.Descriptor = owned
	run, workflow, task, err := AuthoredMaskTransformIDs(plan.Descriptor)
	if err != nil || run != plan.Run || workflow != plan.Workflow || task != plan.Task || draft.ID != task || draft.ParamsArtifactID != plan.Params.ArtifactID || draft.ParamsSHA != plan.Params.SHA256 || string(raw) != plan.ParamsJCS || string(descriptor) != plan.DescriptorJCS || plan.Descriptor.Parent != source.Parent || plan.Descriptor.Instruction != source.Instruction.Identity {
		return v2input.ErrInvalid
	}
	maskRaw, err := canonical.Marshal(plan.Descriptor.SourceMask)
	if err != nil {
		return err
	}
	native, entries, err := v2input.PrepareAuthoredMaskTransformParams(ctx, maskRaw, []byte(targetJCS), plan.Descriptor.Target)
	if err != nil {
		return err
	}
	actualParams, err := canonical.Marshal(native)
	if err != nil || string(actualParams) != string(raw) {
		return v2input.ErrInvalid
	}
	entries = append(entries, v2input.Entry{Ref: plan.Params, Contract: "Params"})
	// SQL compares this sorted exact closure with physical current metadata.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	marker := map[string]any{"descriptor": json.RawMessage(descriptor), "ordinal": source.Instruction.Ordinal, "task_id": task, "params": plan.Params, "params_jcs": string(raw), "input_digest": draft.InputDigest, "native_profile": draft.ExecutionProfile, "input_entries": entries, "target_jcs": targetJCS, "previous_root_jcs": previousRootJCS}
	var snapshot uuid.UUID
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT input_snapshot_id FROM photo.runs WHERE tenant_id=$1 AND id=$2", source.Tenant, source.Parent).Scan(&snapshot)
	})
	if err != nil {
		return err
	}
	if snapshot == uuid.Nil {
		return v2input.ErrInvalid
	}
	config, err := canonical.Marshal(map[string]any{"project_id": source.Project, "input_snapshot_id": snapshot, "authored_mask_transform": marker})
	if err != nil {
		return err
	}
	var alreadySealed bool
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, "SELECT photo.authored_render_mask_transform_sealed_current($1,$2,$3,$4) AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$5 AND r.config_snapshot=$6::jsonb)", source.Tenant, source.Parent, source.Instruction.Ordinal, task, run, string(config)).Scan(&alreadySealed)
	})
	if err != nil {
		return err
	}
	if alreadySealed {
		return ctx.Err()
	}
	var staged bool
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, "SELECT photo.begin_authored_render_mask_transform_task($1,$2,$3,$4,$5,$6::jsonb)", source.Tenant, source.Parent, source.Instruction.Ordinal, plan.Params.ArtifactID, string(config), draft.ExecutionProfile).Scan(&staged)
	})
	if err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	if _, err = p.Params.Publish(ctx, source, plan, plan.Params, raw); err != nil {
		return err
	}
	prepared, err := p.publishTransport(ctx, source, plan)
	if err != nil {
		return err
	}
	return p.seal(ctx, source, plan, prepared)
}

package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Prepare obtains byte counts only from the descriptor-scoped current catalog.
// targetRaw/previousRootJCS are physical documents read through scoped phase
// objects; their exact versioned Ref/JCS authority is independently rechecked by
// SQL. This method neither invents qualification nor marks a task adopted.
func (p AuthoredMaskTransformPreparation) Prepare(ctx context.Context, source AuthoredInstructionSource, descriptor AuthoredMaskTransformDescriptor, targetRaw []byte, previousRootJCS *string, ram, scratch, output int64) (AuthoredMaskTransformPlan, error) {
	var zero AuthoredMaskTransformPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Params.Scheduler != p.Scheduler {
		return zero, v2input.ErrInvalid
	}
	wire, err := canonical.Marshal(descriptor)
	if err != nil {
		return zero, err
	}
	var owned AuthoredMaskTransformDescriptor
	if json.Unmarshal(wire, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	descriptor = owned
	targetRaw = append([]byte(nil), targetRaw...)
	if previousRootJCS != nil {
		copy := *previousRootJCS
		previousRootJCS = &copy
	}
	var raw []byte
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, "SELECT photo.authored_render_mask_transform_input_sizes($1,$2,$3,$4::jsonb,$5,$6)", source.Tenant, source.Parent, source.Instruction.Ordinal, string(wire), string(targetRaw), previousRootJCS).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 4194304 {
		return zero, v2input.ErrExpired
	}
	var catalog struct {
		Params    v2input.Ref     `json:"params"`
		ParamsJCS string          `json:"params_jcs"`
		Entries   []v2input.Entry `json:"entries"`
		Sizes     []struct {
			Ref   v2input.Ref `json:"ref"`
			Bytes int64       `json:"bytes"`
		} `json:"sizes"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&catalog) != nil || dec.Decode(new(any)) != io.EOF {
		return zero, v2input.ErrInvalid
	}
	sizes := make([]v2input.SemanticInputSize, len(catalog.Sizes))
	for i, v := range catalog.Sizes {
		sizes[i] = v2input.SemanticInputSize{Ref: v.Ref, Bytes: v.Bytes}
	}
	plan, err := CompileAuthoredMaskTransformPlan(ctx, source, descriptor, targetRaw, sizes, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	if plan.Params != catalog.Params || plan.ParamsJCS != catalog.ParamsJCS {
		return zero, v2input.ErrInvalid
	}
	// Also compare the exact catalog contract/Ref closure, not only size keys.
	mask, err := canonical.Marshal(descriptor.SourceMask)
	if err != nil {
		return zero, err
	}
	_, entries, err := v2input.PrepareAuthoredMaskTransformParams(ctx, mask, targetRaw, descriptor.Target)
	if err != nil {
		return zero, err
	}
	entries = append(entries, v2input.Entry{Ref: plan.Params, Contract: "Params"})
	expected := map[v2input.Entry]bool{}
	for _, v := range entries {
		expected[v] = true
	}
	if len(catalog.Entries) != len(expected) {
		return zero, v2input.ErrInvalid
	}
	for _, v := range catalog.Entries {
		if !expected[v] {
			return zero, v2input.ErrInvalid
		}
		delete(expected, v)
	}
	if len(expected) != 0 {
		return zero, v2input.ErrInvalid
	}
	return plan, ctx.Err()
}

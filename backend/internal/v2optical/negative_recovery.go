package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type negativeSavedTask struct {
	SchemaVersion int         `json:"schema_version"`
	ParamsRef     v2input.Ref `json:"params_ref"`
	ParamsJCS     string      `json:"params_jcs"`
	SourceJCS     string      `json:"source_jcs"`
	ProfileJCS    string      `json:"profile_jcs"`
	ReservedBytes int64       `json:"reserved_bytes"`
}

// FreezeNegativeTaskDescriptor saves original strings without changing hashes.
// Persistence must be scoped to the durable owner; this compiler performs no IO.
func FreezeNegativeTaskDescriptor(ctx context.Context, plan NegativeTaskPlan, sourceRaw []byte, qualified VerifiedNegativeQualification, params v2input.Ref) ([]byte, error) {
	if _, _, err := NegativeTaskInputEntries(ctx, plan, sourceRaw, qualified, params); err != nil {
		return nil, err
	}
	raw, err := canonical.Marshal(negativeSavedTask{1, params, string(plan.Params), string(sourceRaw), string(plan.Profile), plan.ReservedBytes})
	if err != nil || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	return raw, ctx.Err()
}

// RestoreNegativeTaskDescriptor reconstructs rather than trusting saved fields.
// Current qualification, object versions, source access and physical inputs
// must be resolved/re-locked separately before graph activation.
func RestoreNegativeTaskDescriptor(ctx context.Context, raw []byte, qualified VerifiedNegativeQualification) (NegativeTaskPlan, v2input.Ref, []byte, error) {
	fail := func() (NegativeTaskPlan, v2input.Ref, []byte, error) {
		return NegativeTaskPlan{}, v2input.Ref{}, nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return NegativeTaskPlan{}, v2input.Ref{}, nil, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return fail()
	}
	var saved negativeSavedTask
	if json.Unmarshal(raw, &saved) != nil || saved.SchemaVersion != 1 || saved.ParamsRef.ArtifactID == uuid.Nil {
		return fail()
	}
	encoded, err := canonical.Marshal(saved)
	if err != nil || !bytes.Equal(raw, encoded) {
		return fail()
	}
	if len(saved.ParamsJCS) < 2 || len(saved.ParamsJCS) > 65536 || len(saved.SourceJCS) < 2 || len(saved.SourceJCS) > 65536 || len(saved.ProfileJCS) < 2 || len(saved.ProfileJCS) > 65536 {
		return fail()
	}
	params, err := ParseNegativeParams([]byte(saved.ParamsJCS))
	if err != nil {
		return fail()
	}
	var profile struct {
		Resources NegativeResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal([]byte(saved.ProfileJCS), &profile) != nil {
		return fail()
	}
	plan, err := CompileNegativeTaskPlan(ctx, params.Base, []byte(saved.ParamsJCS), []byte(saved.SourceJCS), qualified, profile.Resources)
	if err != nil {
		return NegativeTaskPlan{}, v2input.Ref{}, nil, err
	}
	if string(plan.Profile) != saved.ProfileJCS || plan.ReservedBytes != saved.ReservedBytes || plan.ParamsSHA != saved.ParamsRef.SHA256 {
		return fail()
	}
	if _, _, err := NegativeTaskInputEntries(ctx, plan, []byte(saved.SourceJCS), qualified, saved.ParamsRef); err != nil {
		return fail()
	}
	return plan, saved.ParamsRef, []byte(saved.SourceJCS), ctx.Err()
}

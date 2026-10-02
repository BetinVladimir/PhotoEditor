package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
)

type SQLLensTaskStage struct{ DB AnalysisStageQuerier }

// Stage uses scheduler credentials after both scoped documents are ready. The
// task remains blocked until exact input transport registration and graph seal.
func (s SQLLensTaskStage) Stage(ctx context.Context, tenant, run, workflow uuid.UUID, ordinal int, frozen FrozenLensTask) error {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return ErrInvalid
	}
	task, err := TaskID(workflow, "optical.lens", ordinal)
	if err != nil {
		return err
	}
	draft, err := frozen.DraftTask()
	if err != nil || task != draft.ID {
		return ErrInvalid
	}
	var accepted bool
	err = s.DB.QueryRow(ctx, `SELECT photo.stage_v25_lens_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
		tenant, run, task, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, json.RawMessage(draft.ExecutionProfile)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return ErrExpired
	}
	return nil
}

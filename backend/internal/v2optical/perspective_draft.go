package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"time"
)

type PerspectiveDraftSnapshot struct {
	Tenant         uuid.UUID       `json:"tenant_id"`
	Project        uuid.UUID       `json:"project_id"`
	Actor          uuid.UUID       `json:"actor_id"`
	Session        uuid.UUID       `json:"auth_session_id"`
	SnapshotID     uuid.UUID       `json:"snapshot_id"`
	Run            uuid.UUID       `json:"run_id"`
	Workflow       uuid.UUID       `json:"workflow_id"`
	Task           uuid.UUID       `json:"task_id"`
	ParamsDocument uuid.UUID       `json:"params_document_id"`
	ParamsJCS      string          `json:"params_jcs"`
	ParamsSHA      string          `json:"params_sha"`
	LinesJCS       string          `json:"lines_jcs,omitempty"`
	SourceJCS      string          `json:"source_jcs"`
	Profile        json.RawMessage `json:"execution_profile"`
	MaxPixels      int64           `json:"max_pixels"`
	ReservedBytes  int64           `json:"reserved_bytes"`
}

// PerspectiveDraft owns compiler output. It is a preliminary draft only; live
// physical verification and current locks must be repeated before admission.
type PerspectiveDraft struct{ raw string }

func (d PerspectiveDraft) Snapshot() (PerspectiveDraftSnapshot, error) {
	var s PerspectiveDraftSnapshot
	if len(d.raw) < 2 || json.Unmarshal([]byte(d.raw), &s) != nil {
		return s, v2input.ErrInvalid
	}
	return s, nil
}
func CompilePerspectiveDraft(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, key string, paramsRaw, sourceRaw []byte, qualified VerifiedPerspectiveQualification, maxPixels int64, capacity PerspectiveResourceEstimate, lineOriginal ...[]byte) (PerspectiveDraft, error) {
	var zero PerspectiveDraft
	if tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	params, err := ParsePerspectiveParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	plan, err := CompilePerspectiveTaskPlan(ctx, params.Base, paramsRaw, sourceRaw, qualified, maxPixels, capacity, lineOriginal...)
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(tenant, []byte("V29.Perspective:"+project.String()+":"+who.UserID.String()+":"+key))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, "optical.perspective", 0)
	if err != nil {
		return zero, err
	}
	s := PerspectiveDraftSnapshot{LinesJCS: plan.LinesJCS, Tenant: tenant, Project: project, Actor: who.UserID, Session: who.SessionID, SnapshotID: snapshot, Run: run, Workflow: workflow, Task: task, ParamsDocument: uuid.NewSHA1(task, []byte("params")), ParamsJCS: string(plan.Params), ParamsSHA: plan.ParamsSHA, SourceJCS: string(sourceRaw), Profile: append(json.RawMessage(nil), plan.Profile...), MaxPixels: maxPixels, ReservedBytes: plan.ReservedBytes}
	body, err := canonical.Marshal(s)
	if err != nil {
		return zero, err
	}
	return PerspectiveDraft{raw: string(body)}, nil
}

// SavePerspectiveDraft never creates runnable tasks or reserves output quota.
func SavePerspectiveDraft(ctx context.Context, db PerspectiveTransactions, who auth.Identity, tenant uuid.UUID, draft PerspectiveDraft) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := draft.Snapshot()
	if err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil || s.Tenant != tenant || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Actor != who.UserID || s.Session != who.SessionID {
		return v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var saved bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v29_perspective_draft($1,$2,$3,$4)", tenant, who.UserID, who.SessionID, draft.raw).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}

// lineOriginals preserves old manual snapshot bytes and owns the auto original.
func (s PerspectiveDraftSnapshot) lineOriginals() [][]byte {
	if s.LinesJCS == "" {
		return nil
	}
	return [][]byte{[]byte(s.LinesJCS)}
}

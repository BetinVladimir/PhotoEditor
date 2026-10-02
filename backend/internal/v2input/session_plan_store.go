package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionPlanLease is a fenced claim on one late-bound plan expansion.
type SessionPlanLease struct {
	ExpansionID uuid.UUID
	Token       uuid.UUID
	Until       time.Time
	TenantID    uuid.UUID
	RunID       uuid.UUID
	WorkflowID  uuid.UUID
}

type SessionPlanStage struct {
	Params      Ref
	InputDigest string
	Shards      []SessionShardResult
	Profile     json.RawMessage
}

type SQLSessionPlanStore struct{ DB *pgxpool.Pool }

// Claim returns nil when no canonical descriptor set is ready. The SQL
// function claims one expansion under a run lock and returns a fresh token.
func (s SQLSessionPlanStore) Claim(ctx context.Context) (*SessionPlanLease, error) {
	if s.DB == nil {
		return nil, ErrInvalid
	}
	var lease SessionPlanLease
	err := s.DB.QueryRow(ctx, `SELECT expansion_id,lease_token,lease_until,
	 tenant_id,run_id,workflow_id FROM photo.claim_v22_session_plan()`).Scan(
		&lease.ExpansionID, &lease.Token, &lease.Until, &lease.TenantID,
		&lease.RunID, &lease.WorkflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validSessionPlanLease(lease) {
		return nil, ErrInvalid
	}
	return &lease, nil
}

func validSessionPlanLease(lease SessionPlanLease) bool {
	return lease.ExpansionID != uuid.Nil && lease.Token != uuid.Nil &&
		lease.TenantID != uuid.Nil && lease.RunID != uuid.Nil &&
		lease.WorkflowID != uuid.Nil && !lease.Until.IsZero()
}

// Inputs re-reads canonical adopted shard outputs. The caller must compare
// these with the frozen descriptor plan before publishing params.
func (s SQLSessionPlanStore) Inputs(ctx context.Context, lease SessionPlanLease) ([]SessionShardResult, error) {
	if s.DB == nil || !validSessionPlanLease(lease) {
		return nil, ErrInvalid
	}
	rows, err := s.DB.Query(ctx, `SELECT task_id,artifact_id,sha256
	 FROM photo.v22_session_plan_inputs($1,$2)`, lease.TenantID, lease.RunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SessionShardResult
	seenTasks := make(map[uuid.UUID]bool)
	seenArtifacts := make(map[uuid.UUID]bool)
	for rows.Next() {
		var item SessionShardResult
		if err := rows.Scan(&item.TaskID, &item.Document.ArtifactID, &item.Document.SHA256); err != nil {
			return nil, err
		}
		expected, err := TaskID(lease.WorkflowID, "session.describe", len(result))
		if err != nil || item.TaskID != expected || !validRef(item.Document) ||
			seenTasks[item.TaskID] || seenArtifacts[item.Document.ArtifactID] || len(result) == 40 {
			return nil, ErrInvalid
		}
		seenTasks[item.TaskID] = true
		seenArtifacts[item.Document.ArtifactID] = true
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrInvalid
	}
	return result, nil
}

// Stage binds the exact published params artifact and shard refs to the
// deterministic session.plan:0 task. A failed result leaves no new task.
func (s SQLSessionPlanStore) Stage(ctx context.Context, lease SessionPlanLease,
	stage SessionPlanStage) (uuid.UUID, error) {
	var profile map[string]json.RawMessage
	if s.DB == nil || !validSessionPlanLease(lease) || !validRef(stage.Params) ||
		!digestPattern.MatchString(stage.InputDigest) || len(stage.Shards) < 1 ||
		len(stage.Shards) > 40 || json.Unmarshal(stage.Profile, &profile) != nil ||
		profile == nil || string(profile["backend"]) != `"cpu"` ||
		string(profile["renderer_version"]) != `"v2.2"` {
		return uuid.Nil, ErrInvalid
	}
	refs := make([]Ref, len(stage.Shards))
	seen := make(map[uuid.UUID]bool, len(refs))
	for i, item := range stage.Shards {
		expected, err := TaskID(lease.WorkflowID, "session.describe", i)
		if err != nil || item.TaskID != expected || !validRef(item.Document) ||
			seen[item.Document.ArtifactID] {
			return uuid.Nil, ErrInvalid
		}
		seen[item.Document.ArtifactID] = true
		refs[i] = item.Document
	}
	taskID, err := TaskID(lease.WorkflowID, "session.plan", 0)
	if err != nil {
		return uuid.Nil, err
	}
	shardsJSON, err := json.Marshal(refs)
	if err != nil {
		return uuid.Nil, err
	}
	var staged bool
	err = s.DB.QueryRow(ctx, `SELECT photo.stage_v22_session_plan(
	 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, lease.TenantID, lease.RunID,
		lease.ExpansionID, lease.Token, taskID, stage.Params.ArtifactID,
		stage.Params.SHA256, stage.InputDigest, json.RawMessage(shardsJSON),
		stage.Profile).Scan(&staged)
	if err != nil {
		return uuid.Nil, err
	}
	if !staged {
		return uuid.Nil, fmt.Errorf("%w: session plan stage rejected", ErrInvalid)
	}
	return taskID, nil
}

func (s SQLSessionPlanStore) Renew(ctx context.Context, lease SessionPlanLease) (bool, error) {
	if s.DB == nil || !validSessionPlanLease(lease) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.renew_v22_session_plan($1,$2)`,
		lease.ExpansionID, lease.Token).Scan(&ok)
	return ok, err
}

func (s SQLSessionPlanStore) Release(ctx context.Context, lease SessionPlanLease) (bool, error) {
	if s.DB == nil || !validSessionPlanLease(lease) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.release_v22_session_plan($1,$2)`,
		lease.ExpansionID, lease.Token).Scan(&ok)
	return ok, err
}

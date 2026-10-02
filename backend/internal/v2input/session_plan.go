package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
)

// SessionDescriptorPlan is the deterministic first stage of a session graph.
// The plan task cannot be materialized until these shards have canonical
// DescriptorShard refs; its params contract requires those exact refs.
type SessionDescriptorPlan struct {
	Tasks []SessionDescriptorTask
}

type SessionDescriptorTask struct {
	ID       uuid.UUID
	NodeKey  string
	Ordinals []int
}

// CompileSessionDescriptors partitions a frozen selection into at most 40
// shards of at most 128 frames. It performs no database or object IO.
func CompileSessionDescriptors(workflow uuid.UUID, itemCount int) (SessionDescriptorPlan, error) {
	if workflow == uuid.Nil || itemCount < 1 || itemCount > 5000 {
		return SessionDescriptorPlan{}, ErrInvalid
	}
	count := (itemCount + 127) / 128
	plan := SessionDescriptorPlan{Tasks: make([]SessionDescriptorTask, 0, count)}
	for shard := 0; shard < count; shard++ {
		key, err := NodeKey("session.describe", shard)
		if err != nil {
			return SessionDescriptorPlan{}, err
		}
		id, err := TaskID(workflow, "session.describe", shard)
		if err != nil {
			return SessionDescriptorPlan{}, err
		}
		start := shard * 128
		end := start + 128
		if end > itemCount {
			end = itemCount
		}
		ordinals := make([]int, end-start)
		for i := range ordinals {
			ordinals[i] = start + i
		}
		plan.Tasks = append(plan.Tasks, SessionDescriptorTask{ID: id, NodeKey: key, Ordinals: ordinals})
	}
	return plan, nil
}

// SessionShardResult must come from a canonical, adopted describe result. The
// caller verifies that database fact before providing each root document ref.
type SessionShardResult struct {
	TaskID   uuid.UUID
	Document Ref
}

type SessionPlanParams struct {
	SchemaVersion int   `json:"schema_version"`
	Shards        []Ref `json:"shards"`
	Policy        any   `json:"policy"`
	Context       Ref   `json:"context"`
}

// CompileSessionPlanParams binds exactly one adopted descriptor per expected
// shard. No placeholder SHA or future artifact reference enters task params.
func CompileSessionPlanParams(descriptors SessionDescriptorPlan, results []SessionShardResult,
	policy any, context Ref) ([]byte, error) {
	if len(descriptors.Tasks) < 1 || len(descriptors.Tasks) > 40 ||
		len(results) != len(descriptors.Tasks) || !validRef(context) || policy == nil {
		return nil, ErrInvalid
	}
	params := SessionPlanParams{SchemaVersion: 1, Shards: make([]Ref, len(results)),
		Policy: policy, Context: context}
	seen := make(map[uuid.UUID]bool, len(results))
	for i, result := range results {
		if result.TaskID != descriptors.Tasks[i].ID || !validRef(result.Document) ||
			seen[result.Document.ArtifactID] {
			return nil, ErrInvalid
		}
		seen[result.Document.ArtifactID] = true
		params.Shards[i] = result.Document
	}
	return canonical.Marshal(params)
}

// PublishSessionPlanParams pins the exact late-bound JSON outside any database
// transaction. The expansion UUID is the document owner across lease retries.
func PublishSessionPlanParams(ctx context.Context, documents DocumentPublisher,
	lease SessionPlanLease, descriptors SessionDescriptorPlan, results []SessionShardResult,
	policy any, planContext Ref) (Ref, error) {
	if documents == nil || !validSessionPlanLease(lease) {
		return Ref{}, ErrInvalid
	}
	body, err := CompileSessionPlanParams(descriptors, results, policy, planContext)
	if err != nil {
		return Ref{}, err
	}
	ref, err := documents.Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID,
		Run: lease.RunID, Owner: lease.ExpansionID,
		Purpose: "v2_session_plan_params"}, body)
	if err != nil {
		return Ref{}, err
	}
	result := Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	if !validRef(result) {
		return Ref{}, ErrInvalid
	}
	digest := sha256.Sum256(body)
	if result.SHA256 != hex.EncodeToString(digest[:]) {
		return Ref{}, ErrInvalid
	}
	return result, nil
}

// CommitSessionPlan registers the previously published input closure and
// closes the leased expansion in one transaction. A failure rolls both back,
// leaving the staged plan task blocked for the same lease or a later retry.
func (a Admission) CommitSessionPlan(ctx context.Context, tenant, run, expansion,
	token uuid.UUID, task GraphTask) error {
	if a.DB == nil {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		return CloseSessionPlanInTransaction(ctx, tx, tenant, run, expansion, token, task)
	})
}

func CloseSessionPlanInTransaction(ctx context.Context, tx pgx.Tx, tenant, run,
	expansion, token uuid.UUID, task GraphTask) error {
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || expansion == uuid.Nil ||
		token == uuid.Nil || task.ID == uuid.Nil {
		return ErrInvalid
	}
	if err := RegisterInTransaction(ctx, tx, tenant, run, task.ID,
		task.Built, task.Open, task.Direct); err != nil {
		return err
	}
	var closed bool
	if err := tx.QueryRow(ctx, "SELECT photo.close_v22_session_plan($1,$2,$3,$4,$5)",
		tenant, run, expansion, token, task.ID).Scan(&closed); err != nil {
		return err
	}
	if !closed {
		return fmt.Errorf("%w: session plan expansion not closed", ErrInvalid)
	}
	return nil
}

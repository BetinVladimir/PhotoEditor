package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// RegisterMultiInputs restores published metadata through current report/catalog
// authority, then registers the complete frozen inventory and seals task inputs
// in one transaction. It never seals the graph or queues execution.
func RegisterMultiInputs(ctx context.Context, db PerspectiveTransactions, bucket string, who auth.Identity, tenant uuid.UUID, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	graph, err := ReadMultiPublishedTransport(ctx, db, bucket, who, tenant, draft, qualified, params)
	if err != nil {
		return zero, err
	}
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return zero, err
	}
	transport, err := canonical.Marshal(struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA      string        `json:"sha256"`
	}{graph.Built.Manifest, graph.Built.Chunks, graph.Built.Count, graph.Built.SHA256})
	if err != nil {
		return zero, err
	}
	manifest, err := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: graph.ID, EntryCount: graph.Built.Count, EntriesSHA: graph.Built.SHA256, Chunks: graph.Built.Chunks})
	if err != nil {
		return zero, err
	}
	s, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	scope := artifactjson.Scope{Tenant: tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	repository := multiParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: draft}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.ParamsJCS)))
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = repository.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var registered bool
	if err = tx.QueryRow(ctx, "SELECT photo.register_v29_multi_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)", tenant, s.Run, string(descriptor), proof, string(transport), string(manifest)).Scan(&registered); err != nil {
		return zero, err
	}
	if !registered {
		return zero, v2input.ErrExpired
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return graph, nil
}

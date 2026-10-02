package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
)

type ActivationRows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type IngestActivationSource struct{ DB ActivationRows }

func (s IngestActivationSource) Pending(ctx context.Context, limit int) ([]BoundTaskActivation, error) {
	if s.DB == nil || limit < 1 || limit > 100 {
		return nil, errors.New("invalid ingest activation query")
	}
	rows, err := s.DB.Query(ctx, "SELECT task_id,params_id,input_digest FROM photo.pending_ingest_activations($1)", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoundTaskActivation{}
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var a BoundTaskActivation
		if err = rows.Scan(&a.TaskID, &a.ParamsID, &a.InputDigest); err != nil {
			return nil, err
		}
		if _, err = domain.ParseDigest(a.InputDigest); err != nil || a.TaskID == uuid.Nil || a.ParamsID == uuid.Nil || seen[a.TaskID] || len(out) >= limit {
			return nil, errors.New("invalid ingest activation row")
		}
		seen[a.TaskID] = true
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type PendingIngestTasks interface {
	Pending(context.Context, int) ([]BoundTaskActivation, error)
}
type BoundTaskActivator interface {
	Activate(context.Context, BoundTaskActivation) (bool, error)
}
type IngestActivator struct {
	Source PendingIngestTasks
	Tasks  BoundTaskActivator
}

// Advance counts successful activation confirmations, including an idempotent
// ready replay if another scheduler wins. It does not count broker dispatches.
func (a IngestActivator) Advance(ctx context.Context, limit int) (int, error) {
	if a.Source == nil || a.Tasks == nil || limit < 1 || limit > 100 {
		return 0, errors.New("invalid ingest activator")
	}
	pending, err := a.Source.Pending(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(pending) > limit {
		return 0, errors.New("activation batch exceeded limit")
	}
	count := 0
	for _, task := range pending {
		if err = ctx.Err(); err != nil {
			return count, err
		}
		ok, err := a.Tasks.Activate(ctx, task)
		if err != nil {
			return count, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}

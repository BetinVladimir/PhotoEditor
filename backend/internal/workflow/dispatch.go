package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

type ResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

func ParseResourceEstimate(profile []byte) (ResourceEstimate, error) {
	var p map[string]json.RawMessage
	var e ResourceEstimate
	if err := json.Unmarshal(profile, &p); err != nil {
		return e, errors.New("invalid execution profile")
	}
	raw, ok := p["resource_estimate"]
	if !ok {
		return e, errors.New("missing resource estimate")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 5 || keys["ram_bytes"] == nil || keys["vram_bytes"] == nil || keys["scratch_bytes"] == nil || keys["output_bytes"] == nil || keys["pixel_count"] == nil {
		return e, errors.New("incomplete resource estimate")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	for _, value := range keys {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return e, errors.New("null resource estimate")
		}
	}
	if err := d.Decode(&e); err != nil {
		return e, errors.New("invalid resource estimate")
	}
	const max = int64(1<<53 - 1)
	if e.RAMBytes <= 0 || e.RAMBytes > max || e.VRAMBytes < 0 || e.VRAMBytes > max || e.ScratchBytes < 0 || e.ScratchBytes > max || e.OutputBytes < 0 || e.OutputBytes > max || e.PixelCount < 0 || e.PixelCount > max {
		return e, errors.New("invalid resource estimate range")
	}
	pixelLimit := int64(200000000)
	if raw, ok := p["max_pixels"]; ok {
		if json.Unmarshal(raw, &pixelLimit) != nil || pixelLimit < 1 || pixelLimit > max {
			return e, errors.New("invalid pixel limit")
		}
	}
	if e.PixelCount > pixelLimit {
		return e, errors.New("output pixels limit")
	}
	return e, nil
}

type Dispatcher struct{ DB *pgxpool.Pool; LegacyOnly bool; PreferModelAffinity bool }

func (d Dispatcher) Dispatch(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, errors.New("invalid dispatch limit")
	}
	count := 0
	for i := 0; i < limit; i++ {
		picked, queued, err := d.dispatchOne(ctx)
		if err != nil {
			return count, err
		}
		if queued {
			count++
		}
		if !picked {
			break
		}
	}
	return count, nil
}
func (d Dispatcher) dispatchOne(ctx context.Context) (picked, queued bool, err error) {
	err = database.InTransaction(ctx, d.DB, func(tx pgx.Tx) error {
		var e error
		picked, queued, e = d.dispatchInTransaction(ctx, tx)
		return e
	})
	return picked, queued, err
}
func (d Dispatcher) dispatchInTransaction(ctx context.Context, tx pgx.Tx) (picked, queued bool, err error) {
	var id uuid.UUID
	var class string
	var profile []byte
	query:="SELECT task_id,resource_class,profile FROM photo.pick_ready_task()"
	if d.LegacyOnly {query="SELECT task_id,resource_class,profile FROM photo.pick_ready_legacy_task()"}
	err = tx.QueryRow(ctx, query).Scan(&id, &class, &profile)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return d.dispatchSelectedInTransaction(ctx,tx,id,profile)
}

// Shared admission/outbox path for legacy selection and leader-owned V32 DRR.
func (d Dispatcher) dispatchSelectedInTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID, profile []byte) (picked, queued bool, err error) {
	estimate, estimateErr := ParseResourceEstimate(profile)
	reason := "waiting_worker"
	var nodes []string
	if estimateErr == nil {
		nodeQuery := "SELECT node_id FROM photo.eligible_dispatch_nodes($1)"
		if d.PreferModelAffinity { nodeQuery = "SELECT node_id FROM photo.capacity_affinity_dispatch_nodes($1)" }
		rows, e := tx.Query(ctx, nodeQuery, id)
		if e != nil {
			return true, false, e
		}
		for rows.Next() {
			var node string
			if e = rows.Scan(&node); e != nil {
				rows.Close()
				return true, false, e
			}
			nodes = append(nodes, node)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return true, false, e
		}
	} else {
		reason = "invalid_resource_estimate"
	}
	event, correlation := uuid.New(), uuid.New()
	for _, node := range nodes {
		// Rollback a failed candidate's savepoint releases its node/slot/domain locks
		// before trying another node. The fairness row remains held by the outer tx.
		candidate, e := tx.Begin(ctx)
		if e != nil {
			return true, false, e
		}
		var outcome string
		var command []byte
		e = candidate.QueryRow(ctx, "SELECT outcome,command FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", id, node, estimate.RAMBytes, estimate.VRAMBytes, estimate.ScratchBytes, event, correlation, "").Scan(&outcome, &command)
		if e != nil {
			candidate.Rollback(context.WithoutCancel(ctx))
			return true, false, e
		}
		if outcome == "queued" {
			if e = candidate.Commit(ctx); e != nil {
				return true, false, e
			}
			var recorded bool
			if e = tx.QueryRow(ctx, "SELECT photo.record_dispatch($1,$2)", id, event).Scan(&recorded); e != nil {
				return true, false, e
			}
			if !recorded {
				return true, false, errors.New("dispatch fairness record missing")
			}
			return true, true, nil
		}
		if e = candidate.Rollback(ctx); e != nil {
			return true, false, e
		}
		switch outcome {
		case "waiting_worker", "waiting_resource", "waiting_quota", "unsupported_backend", "unqualified_model", "input_unavailable":
			reason = outcome
		case "stale", "cancelled", "unsealed", "missing":
			return true, false, nil
		default:
			return true, false, errors.New("unknown resource admission outcome")
		}
	}
	var deferred bool
	if err = tx.QueryRow(ctx, "SELECT photo.defer_dispatch($1,$2)", id, reason).Scan(&deferred); err != nil {
		return true, false, err
	}
	return true, false, nil
}

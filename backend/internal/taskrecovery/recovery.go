package taskrecovery

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// Recovery only changes durable task state. It never publishes commands or
// repeats an operation; ready tasks still pass through resource admission.
// RecoveryDB permits caller-owned transactions without changing the default
// pool's statement/commit boundaries. A transaction caller owns rollback/commit.
type RecoveryDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type Recovery struct {
	DB  RecoveryDB
	Now func() time.Time
}

// Recover drains idle cancellations, reaps dead leases, decides each pending
// failure, then releases due retries. Decisions are fenced CAS writes, so multiple schedulers are safe.
func (r Recovery) Recover(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, errors.New("invalid recovery limit")
	}
	var count int
	withdrawn, err := ProcessConsentMaintenance(ctx, r.DB, []string{
		"SELECT photo.withdraw_completed_glare_consent()",
		"SELECT photo.withdraw_completed_open_eyes_consent()",
	}, limit)
	count += withdrawn
	if err != nil {
		return count, err
	}
	// With the default pool, separate statements release task/resource locks
	// between candidates. A caller-owned transaction deliberately retains them
	// until its owner commits or rolls back.
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.cancel_ingest_upload()").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.finalize_run_cancellation()").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.reap_tasks(1)").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	settled, err := settleConsentQuotas(ctx, r.DB, limit)
	count += settled
	if err != nil {
		return count, err
	}
	rows, err := r.DB.Query(ctx, "SELECT task_id,attempt,fence,retry_count,oom_count,cancel_requested,error,profile FROM photo.pending_task_failures($1)", limit)
	if err != nil {
		return count, err
	}
	type pending struct {
		id                    uuid.UUID
		attempt, retries, oom int
		fence                 int64
		cancelled             bool
		errorRaw, profileRaw  []byte
	}
	var failures []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.attempt, &p.fence, &p.retries, &p.oom, &p.cancelled, &p.errorRaw, &p.profileRaw); err != nil {
			rows.Close()
			return count, err
		}
		failures = append(failures, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return count, err
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	for _, p := range failures {
		var failure struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		}
		var profile map[string]json.RawMessage
		if json.Unmarshal(p.errorRaw, &failure) != nil || json.Unmarshal(p.profileRaw, &profile) != nil {
			return count, errors.New("invalid persisted failure")
		}
		reduced, canReduce := ReduceExecutionProfile(profile)
		fallback, canFallback := CPUFallbackProfile(profile)
		decision := DecideRetry(RetryContext{TaskID: p.id, Attempt: p.attempt, RetryCount: p.retries, OOMCount: p.oom, CancelRequested: p.cancelled, CanReduce: canReduce, CanFallbackCPU: canFallback, Now: now}, Failure{Code: failure.Code, Retryable: failure.Retryable})
		nextProfile := p.profileRaw
		if decision.ReduceResources {
			nextProfile = reduced
		}
		if decision.FallbackCPU {
			nextProfile = fallback
		}
		var applied bool
		err = r.DB.QueryRow(ctx, "SELECT photo.plan_task_retry($1,$2,$3,$4,$5,$6,$7,$8,$9)", p.id, p.attempt, p.fence, decision.State, decision.RetryCount, decision.OOMCount, decision.NextAttemptAt, nextProfile, decision.Reason).Scan(&applied)
		if err != nil {
			return count, err
		}
		if applied {
			count++
		}
	}
	// Renew only existing cleanup-held run capacity before the activation gate skips waiters.
	for i := 0; i < limit; i++ {
		var renewed bool
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := r.DB.QueryRow(bounded, "SELECT photo.renew_next_retry_cleanup_quota()").Scan(&renewed)
		late := bounded.Err()
		cancel()
		if err != nil {
			return count, err
		}
		if late != nil {
			return count, late
		}
		if !renewed {
			break
		}
		count++
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.activate_task_retries(1)").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.finalize_next_semantic_apply_child()").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	for i := 0; i < limit; i++ {
		var n int
		if err := r.DB.QueryRow(ctx, "SELECT photo.settle_next_semantic_child()").Scan(&n); err != nil {
			return count, err
		}
		count += n
		if n == 0 {
			break
		}
	}
	return count, nil
}

func cloneProfile(p map[string]json.RawMessage) map[string]json.RawMessage {
	q := make(map[string]json.RawMessage, len(p))
	for k, v := range p {
		q[k] = v
	}
	return q
}

// Resource reductions are restricted to explicitly declared safe limits. A
// model without such a declaration is never silently reconfigured.
func ReduceExecutionProfile(p map[string]json.RawMessage) ([]byte, bool) {
	q := cloneProfile(p)
	var batch, minBatch, tile, minTile int
	json.Unmarshal(p["batch_size"], &batch)
	json.Unmarshal(p["min_batch_size"], &minBatch)
	json.Unmarshal(p["tile_size"], &tile)
	json.Unmarshal(p["min_tile_size"], &minTile)
	if minBatch > 0 && batch > minBatch {
		v := batch / 2
		if v < minBatch {
			v = minBatch
		}
		q["batch_size"], _ = json.Marshal(v)
	} else if minTile >= 64 && tile > minTile {
		v := tile / 2
		if v < minTile {
			v = minTile
		}
		q["tile_size"], _ = json.Marshal(v)
	} else {
		return nil, false
	}
	b, err := json.Marshal(q)
	return b, err == nil
}
func CPUFallbackProfile(p map[string]json.RawMessage) ([]byte, bool) {
	var allow, qualified bool
	var backend string
	json.Unmarshal(p["allow_cpu_fallback"], &allow)
	json.Unmarshal(p["cpu_qualified"], &qualified)
	json.Unmarshal(p["backend"], &backend)
	if !allow || !qualified || (backend != "mps" && backend != "cuda") {
		return nil, false
	}
	estimate, ok := p["cpu_resource_estimate"]
	if !ok {
		return nil, false
	}
	validation := map[string]json.RawMessage{"resource_estimate": estimate}
	if max, ok := p["max_pixels"]; ok {
		validation["max_pixels"] = max
	}
	check, _ := json.Marshal(validation)
	if _, err := ParseResourceEstimate(check); err != nil {
		return nil, false
	}
	q := cloneProfile(p)
	q["resource_estimate"] = estimate
	q["backend"] = json.RawMessage(`"cpu"`)
	q["precision"] = json.RawMessage(`"fp32"`)
	b, err := json.Marshal(q)
	return b, err == nil
}

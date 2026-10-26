package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

const retryTask = "aa4e6164-4366-5cee-a1d3-cafaae8333f0"

func recoveryFields(phase string) []string {
	if phase == "plan" {
		return []string{"next_attempt_at", "retry_count", "terminal_reason"}
	}
	return []string{"state", "next_attempt_at", "dispatch_command", "terminal_reason"}
}
func recoveryTask(ctx context.Context, tx pgx.Tx) (map[string]json.RawMessage, time.Time, error) {
	var raw []byte
	var now time.Time
	e := tx.QueryRow(ctx, "SELECT to_jsonb(t)||jsonb_build_object('_failure_error',a.error,'_failure_finished_at',a.finished_at,'_failure_fence',a.fencing_token,'_failure_worker',a.worker_id),clock_timestamp() FROM photo.tasks t JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt WHERE t.id=$1::uuid FOR UPDATE OF t", retryTask).Scan(&raw, &now)
	var row map[string]json.RawMessage
	if e == nil {
		e = json.Unmarshal(raw, &row)
	}
	return row, now, e
}
func recoveryInt(r map[string]json.RawMessage, k string) int {
	var n int
	_ = json.Unmarshal(r[k], &n)
	return n
}
func recoveryNull(r map[string]json.RawMessage, k string) bool { return string(r[k]) == "null" }
func recoveryDate(r map[string]json.RawMessage) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, text(r, "next_attempt_at"))
}
func recoveryBefore(r map[string]json.RawMessage, phase string, now time.Time) error {
	var failure struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	_ = json.Unmarshal(r["_failure_error"], &failure)
	if text(r, "id") != retryTask || text(r, "run_id") != "e991510e-6de9-4aa3-ad25-2e9192a49f6d" || text(r, "tenant_id") != "5dbe7f17-b560-4696-9525-03959e843841" || text(r, "state") != "retry_wait" || recoveryInt(r, "attempt") != 1 || recoveryInt(r, "fencing_token") != 1 || string(r["cancel_requested"]) != "false" || failure.Code != "DEPENDENCY_UNAVAILABLE" || !failure.Retryable || recoveryNull(r, "_failure_finished_at") || recoveryInt(r, "_failure_fence") != 1 || text(r, "_failure_worker") != text(r, "worker_id") {
		return errors.New("saved TIFF failure authority drift")
	}
	if phase == "plan" {
		if !recoveryNull(r, "next_attempt_at") || recoveryInt(r, "retry_count") != 0 {
			return errors.New("not unplanned saved failure")
		}
	} else if phase == "activate" {
		next, e := recoveryDate(r)
		if e != nil || next.After(now) || recoveryInt(r, "retry_count") != 1 {
			return errors.New("retry not due")
		}
	} else {
		return errors.New("unknown recovery phase")
	}
	return nil
}
func validateRecoveryEffect(before, after map[string]json.RawMessage, phase string, count int, now time.Time) error {
	if count != 1 {
		return errors.New("combined or unrelated Recovery work refused")
	}
	if phase == "plan" {
		next, e := recoveryDate(after)
		if e != nil || !next.After(now) || next.After(now.Add(150*time.Second)) || text(after, "state") != "retry_wait" || recoveryInt(after, "retry_count") != 1 || text(after, "terminal_reason") != "DEPENDENCY_UNAVAILABLE" {
			return errors.New("normal retry plan effect refused")
		}
	} else if phase == "activate" {
		if text(after, "state") != "ready" || !recoveryNull(after, "next_attempt_at") || !recoveryNull(after, "dispatch_command") || !recoveryNull(after, "terminal_reason") {
			return errors.New("normal retry activation effect refused")
		}
	} else {
		return errors.New("unknown recovery phase")
	}
	a := map[string]json.RawMessage{}
	b := map[string]json.RawMessage{}
	for k, v := range before {
		a[k] = v
	}
	for k, v := range after {
		b[k] = v
	}
	for _, k := range recoveryFields(phase) {
		delete(a, k)
		delete(b, k)
	}
	if !reflect.DeepEqual(a, b) {
		return errors.New("immutable retry task field drift")
	}
	return nil
}

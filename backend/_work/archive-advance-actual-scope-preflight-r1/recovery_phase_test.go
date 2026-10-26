package main

import (
	"encoding/json"
	"testing"
	"time"
)

func retryFixture(now time.Time) map[string]json.RawMessage {
	r := map[string]json.RawMessage{}
	v := map[string]any{"id": retryTask, "run_id": "e991510e-6de9-4aa3-ad25-2e9192a49f6d", "tenant_id": "5dbe7f17-b560-4696-9525-03959e843841", "state": "retry_wait", "attempt": 1, "fencing_token": 1, "retry_count": 0, "oom_count": 0, "cancel_requested": false, "next_attempt_at": nil, "terminal_reason": "old", "dispatch_command": "old", "worker_id": "worker", "_failure_worker": "worker", "_failure_fence": 1, "_failure_finished_at": now.Format(time.RFC3339Nano), "_failure_error": map[string]any{"code": "DEPENDENCY_UNAVAILABLE", "retryable": true}, "execution_profile": map[string]any{"exact": "original"}}
	for k, x := range v {
		r[k], _ = json.Marshal(x)
	}
	return r
}
func cloneRetry(r map[string]json.RawMessage) map[string]json.RawMessage {
	v := map[string]json.RawMessage{}
	for k, x := range r {
		v[k] = x
	}
	return v
}
func setRetry(r map[string]json.RawMessage, k string, v any) { r[k], _ = json.Marshal(v) }
func TestRecoveryPhasePlanAndActivate(t *testing.T) {
	now := time.Now().UTC()
	old := retryFixture(now)
	if recoveryBefore(old, "plan", now) != nil {
		t.Fatal("plan pre")
	}
	plan := cloneRetry(old)
	setRetry(plan, "retry_count", 1)
	setRetry(plan, "next_attempt_at", now.Add(time.Second).Format(time.RFC3339Nano))
	setRetry(plan, "terminal_reason", "DEPENDENCY_UNAVAILABLE")
	if e := validateRecoveryEffect(old, plan, "plan", 1, now); e != nil {
		t.Fatal(e)
	}
	if recoveryBefore(plan, "activate", now) == nil {
		t.Fatal("not due")
	}
	if e := recoveryBefore(plan, "activate", now.Add(2*time.Second)); e != nil {
		t.Fatal(e)
	}
	ready := cloneRetry(plan)
	setRetry(ready, "state", "ready")
	for _, k := range []string{"next_attempt_at", "dispatch_command", "terminal_reason"} {
		setRetry(ready, k, nil)
	}
	if e := validateRecoveryEffect(plan, ready, "activate", 1, now.Add(2*time.Second)); e != nil {
		t.Fatal(e)
	}
	if validateRecoveryEffect(old, ready, "plan", 2, now) == nil {
		t.Fatal("combined committed")
	}
}
func TestRecoveryPhaseImmutableFields(t *testing.T) {
	now := time.Now().UTC()
	old := retryFixture(now)
	plan := cloneRetry(old)
	setRetry(plan, "retry_count", 1)
	setRetry(plan, "next_attempt_at", now.Add(time.Second).Format(time.RFC3339Nano))
	setRetry(plan, "terminal_reason", "DEPENDENCY_UNAVAILABLE")
	for _, k := range []string{"attempt", "fencing_token", "worker_id", "execution_profile", "_failure_error"} {
		bad := cloneRetry(plan)
		setRetry(bad, k, "changed")
		if validateRecoveryEffect(old, bad, "plan", 1, now) == nil {
			t.Fatalf("accepted %s", k)
		}
	}
}

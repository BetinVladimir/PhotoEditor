package main

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func retryScopeFixture() scope {
	s := syntheticScope()
	s.Run = retryableRun
	s.Phase = "quality"
	for i := 0; i < 9; i++ {
		s.Tasks = append(s.Tasks, taskBinding{ID: uuid.New(), Operation: "image.local", Class: "cpu"})
	}
	s.Tasks[0].ID = retryableTask
	return s
}
func TestRetryScopeExactIdentity(t *testing.T) {
	s := retryScopeFixture()
	if retryableScope(s, "recover_retryable_plan") != nil {
		t.Fatal("valid scope")
	}
	for _, change := range []func(*scope){func(s *scope) { s.Run = oldApplyRun }, func(s *scope) { s.Phase = "local" }, func(s *scope) { s.Tasks = s.Tasks[:8] }, func(s *scope) { s.Tasks[0].Operation = "quality.check" }, func(s *scope) { s.Tasks[0].Class = "media" }, func(s *scope) { s.Tasks[0].ID = uuid.New() }} {
		bad := retryScopeFixture()
		change(&bad)
		if retryableBefore(context.Background(), nil, bad, "recover_retryable_plan") == nil {
			t.Fatal("foreign scope admitted")
		}
	}
}
func TestRetryRejectsUnexpectedCountBeforeQuery(t *testing.T) {
	for _, n := range []int{0, 2, 3} {
		if retryableAfter(context.Background(), nil, retryScopeFixture(), "recover_retryable_plan", n) == nil {
			t.Fatal("multi-effect accepted")
		}
	}
}
func rawRetryRow(s scope) map[string]json.RawMessage {
	b, _ := json.Marshal(map[string]any{"id": retryableTask, "tenant_id": s.Tenant, "run_id": s.Run, "state": "retry_wait", "retry_count": 0, "next_attempt_at": nil, "attempt": 1, "fencing_token": 1, "worker_id": "worker-old", "dispatch_worker_id": "dispatch-old", "dispatch_command": "original", "execution_profile": map[string]any{"ram_bytes": 99}, "terminal_reason": "DEPENDENCY_UNAVAILABLE", "unknown_future": "protected"})
	var row map[string]json.RawMessage
	json.Unmarshal(b, &row)
	return row
}
func TestRetryPlanningPreservesHistoryAndOtherOwnTasks(t *testing.T) {
	s := retryScopeFixture()
	s.retryMode = "recover_retryable_plan"
	a := rawRetryRow(s)
	b := rawRetryRow(s)
	b["retry_count"] = json.RawMessage(`1`)
	b["next_attempt_at"] = json.RawMessage(`"future"`)
	s.normalize("tasks", a)
	s.normalize("tasks", b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("normal plan fields not isolated")
	}
	for _, k := range []string{"attempt", "fencing_token", "worker_id", "dispatch_worker_id", "dispatch_command", "execution_profile", "terminal_reason", "unknown_future"} {
		a := rawRetryRow(s)
		b := rawRetryRow(s)
		b[k] = json.RawMessage(`"drift"`)
		s.normalize("tasks", a)
		s.normalize("tasks", b)
		if reflect.DeepEqual(a, b) {
			t.Fatal("history drift hidden", k)
		}
	}
	a = rawRetryRow(s)
	a["id"] = json.RawMessage(`"other-own-task"`)
	before, _ := json.Marshal(a)
	skip, e := s.normalize("tasks", a)
	after, _ := json.Marshal(a)
	if e != nil || skip || string(before) != string(after) {
		t.Fatal("other own task not wholly protected")
	}
}
func TestRetryActivationOnlyNormalFields(t *testing.T) {
	s := retryScopeFixture()
	s.retryMode = "recover_retryable_activate"
	a := rawRetryRow(s)
	b := rawRetryRow(s)
	b["state"] = json.RawMessage(`"ready"`)
	b["next_attempt_at"] = json.RawMessage(`null`)
	b["terminal_reason"] = json.RawMessage(`null`)
	b["dispatch_command"] = json.RawMessage(`null`)
	s.normalize("tasks", a)
	s.normalize("tasks", b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("normal activation fields not isolated")
	}
	a = rawRetryRow(s)
	s.normalize("task_attempts", a)
	if _, ok := a["retry_count"]; !ok {
		t.Fatal("attempt history normalized")
	}
}

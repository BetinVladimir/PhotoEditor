package queue

import (
	"os"
	"strings"
	"testing"
)

// Unit verifies actual delivery function treats stale/duplicate as committed ACK,
// never a task/graph mutation. Authority predicates themselves need isolated SQL tests.
func TestFocusedSQLPrivateAndAlreadyPublishedEffects(t *testing.T) {
	raw, e := os.ReadFile("/Users/freelancer/Documents/PhotoEditor/work/v21-event-progress-2657-source-r2/02657_orchestrator_event_receipts_progress.sql")
	if e != nil {
		t.Fatal(e)
	}
	s := string(raw)
	a := strings.Index(s, "CREATE FUNCTION photo.accept_orchestrator_event")
	b := strings.Index(s[a:], "-- +goose StatementEnd")
	fn := s[a : a+b]
	for _, forbidden := range []string{"UPDATE photo.tasks", "UPDATE photo.runs", "UPDATE photo.apply_builds", "plan_task_retry", "commit_task_success", "release_task_resources", "publish_download"} {
		if strings.Contains(fn, forbidden) {
			t.Fatal("second lifecycle writer", forbidden)
		}
	}
	if !strings.Contains(fn, "IF r.created_by IS NULL THEN RETURN 'accepted';END IF;") || !strings.Contains(fn, "r.created_by,'principal',projection,body") {
		t.Fatal("private/nullowner contract changed")
	}
	if !strings.Contains(fn, "-- download_status producer already wrote principal run_events in the same commit.\n  projection:=NULL;") {
		t.Fatal("download duplicate projection")
	}
	if !strings.Contains(fn, "RETURN 'stale'") || !strings.Contains(fn, "x.id=t.canonical_result_id") || !strings.Contains(fn, "prior IS DISTINCT FROM hash") {
		t.Fatal("fence/canonical/digest contract missing")
	}
}

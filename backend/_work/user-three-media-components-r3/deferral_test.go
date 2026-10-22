package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/taskrecovery"
	"strings"
	"testing"
	"time"
)

func expandedFixture(t *testing.T) scope {
	t.Helper()
	s := fixture(t)
	raw, e := os.ReadFile("/Users/freelancer/Documents/PhotoEditor/work/rtx-initial-media-components-r2/six-normal-defer-scope.json")
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	if e = s.withDeferrals(raw, hex.EncodeToString(h[:])); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestSixDeferralsRequireExactBindingsAndRemainMetadataProtected(t *testing.T) {
	s := expandedFixture(t)
	if len(s.deferred) != 6 {
		t.Fatal("six required")
	}
	for id, b := range s.deferred {
		v := map[string]string{"id": id, "tenant_id": b.Tenant, "run_id": b.Run, "operation": "asset.verify", "resource_class": "media", "state": "ready", "inputs": "immutable-a"}
		before := row(v)
		after := row(v)
		after["admission_after"] = json.RawMessage(`"later"`)
		after["terminal_reason"] = json.RawMessage(`"waiting_worker"`)
		for _, r := range []map[string]json.RawMessage{before, after} {
			skip, e := s.normalize("tasks", r)
			if e != nil || skip {
				t.Fatal("six row must remain protected", e)
			}
		}
		a, _ := json.Marshal(before)
		z, _ := json.Marshal(after)
		if string(a) != string(z) {
			t.Fatal("two legitimate defer fields not normalized")
		}
		after["inputs"] = json.RawMessage(`"immutable-b"`)
		z, _ = json.Marshal(after)
		if string(a) == string(z) {
			t.Fatal("immutable changes ignored")
		}
		for _, key := range []string{"tenant_id", "run_id", "operation", "resource_class", "state", "terminal_reason"} {
			r := row(v)
			r[key] = json.RawMessage(`"foreign"`)
			if _, e := s.normalize("tasks", r); e == nil {
				t.Fatal("invalid six field allowed", key)
			}
		}
		break
	}
}
func TestSelectedDeferralReadbackRejectsImmutableNonselectedAndTimestampDelta(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(2 * time.Second)
	reason := "waiting_worker"
	before := []deferralRow{{Task: "a", Immutable: "sha"}, {Task: "b", Immutable: "sha2"}}
	after := append([]deferralRow(nil), before...)
	after[0].When = &later
	after[0].Reason = &reason
	selected := map[string]bool{"a": true}
	if e := validateDeferrals(before, after, selected, now, now); e != nil {
		t.Fatal(e)
	}
	after[1].When = &later
	if validateDeferrals(before, after, selected, now, now) == nil {
		t.Fatal("nonselected mutation")
	}
	after[1] = before[1]
	after[0].Immutable = "drift"
	if validateDeferrals(before, after, selected, now, now) == nil {
		t.Fatal("immutable drift")
	}
	after[0].Immutable = "sha"
	far := now.Add(time.Hour)
	after[0].When = &far
	if validateDeferrals(before, after, selected, now, now) == nil {
		t.Fatal("non-normal timestamp")
	}
}

type outsideTx struct {
	pgx.Tx
	ids, terminal []string
	queries       int
}

func (t *outsideTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	t.queries++
	if strings.Contains(q, "FROM photo.tasks") {
		t.ids = a[0].([]string)
		t.terminal = a[1].([]string)
	}
	return controlRow(func(d ...any) error { *d[0].(*int) = 0; return nil })
}
func TestActualOutsideGuardIncludesSkippedTerminalAndClosed74Only(t *testing.T) {
	s := expandedFixture(t)
	tx := &outsideTx{}
	if e := outsideZero(context.Background(), tx, s); e != nil {
		t.Fatal(e)
	}
	if len(tx.ids) != 18 || len(tx.terminal) != 4 || tx.queries != 2 {
		t.Fatal("outside guard cardinality")
	}
	found := false
	for _, state := range tx.terminal {
		if !taskrecovery.TaskState(state).Terminal() {
			t.Fatal("nonterminal excluded")
		}
		if state == string(taskrecovery.Skipped) {
			found = true
		}
	}
	if !found {
		t.Fatal("skipped overcounted")
	}
}
func TestNormalDispatchOneOwnSharesOneSnapshotAndRoleWindow(t *testing.T) {
	s := fixture(t)
	tx := &componentTx{}
	for id := range s.tasks {
		tx.selected = uuid.MustParse(id)
		break
	}
	if n, e := one(context.Background(), tx, s, "dispatch"); e != nil || n != 0 {
		t.Fatal("normal own defer batch", e)
	}
	pick, role, reset, catalog := 0, 0, 0, 0
	for _, q := range tx.calls {
		switch {
		case strings.Contains(q, "photo.pick_ready_task()"):
			pick++
		case q == "SET LOCAL ROLE photo_scheduler":
			role++
		case q == "RESET ROLE":
			reset++
		case strings.Contains(q, "FROM pg_class"):
			catalog++
		}
	}
	if pick != 1 || role != 1 || reset != 1 || catalog != 2 {
		t.Fatal("batch split or unbounded", pick, role, reset, catalog)
	}
}

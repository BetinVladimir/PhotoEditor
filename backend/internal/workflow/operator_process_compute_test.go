package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func TestOperatorObservationEnvelopeIsClosed(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"unexpected":true}`, `{"schema_version":1} {"schema_version":2}`} {
		var v struct {
			Schema int `json:"schema_version"`
		}
		if decodeOperatorClosed([]byte(raw), &v) == nil {
			t.Fatalf("accepted open or trailing document: %s", raw)
		}
	}
	var v struct {
		Schema int `json:"schema_version"`
	}
	if e := decodeOperatorClosed([]byte(`{"schema_version":1}`), &v); e != nil || v.Schema != 1 {
		t.Fatal(v, e)
	}
}
func TestOperatorRequestCreationIsExclusiveAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	first := []byte("{\"a\":1}")
	if e := writeOperatorExclusive(path, first); e != nil {
		t.Fatal(e)
	}
	if e := writeOperatorExclusive(path, []byte("replacement")); e == nil {
		t.Fatal("overwrote retained request")
	}
	body, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(body, first) {
		t.Fatal("first request changed", e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("not private from creation", e)
	}
}
func TestOperatorProcessRequiresDeadlineBeforeSpawning(t *testing.T) {
	_, e := (&OperatorProcessCompute{}).Execute(context.Background(), PreparedTask{})
	if e == nil || e.Error() != "operator process deadline required" {
		t.Fatalf("missing deadline was not refused first: %v", e)
	}
}
func TestOperatorProcessInputBudgetIncludesRetainedNonTaskObjects(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ledger := &OperatorApplyLedger{objects: map[uuid.UUID]styleArtifact{first: {ArtifactID: first, SHA256: digest, Bytes: 8}, second: {ArtifactID: second, SHA256: digest, Bytes: 16}}, outputs: map[uuid.UUID][]OperatorMeasuredOutput{}}
	task := PreparedTask{}
	task.Params.ID = first
	task.Params.SHA256 = digest
	descriptors, e := (&OperatorProcessCompute{Ledger: ledger}).descriptors(task)
	if e != nil || len(descriptors) != 2 {
		t.Fatal("retained byte budget narrowed to task inputs", e)
	}
	raw, e := json.Marshal(descriptors)
	if e != nil || !bytes.Contains(raw, []byte(second.String())) {
		t.Fatal("orphaned retained object", e)
	}
	task.Params.SHA256 = "changed"
	if _, e = (&OperatorProcessCompute{Ledger: ledger}).descriptors(task); e == nil {
		t.Fatal("accepted changed compiler Params")
	}
}

func TestOperatorBoundIdentityExactMeasuredLease(t *testing.T) {
	lease := ApplyLease{TenantID: uuid.New(), RunID: uuid.New()}
	exact := operatorBoundIdentity{lease.TenantID, lease.RunID}
	if !operatorIdentityMatches(exact, lease) {
		t.Fatal("exact measured identity rejected")
	}
	for _, got := range []operatorBoundIdentity{{uuid.New(), lease.RunID}, {lease.TenantID, uuid.New()}, {uuid.Nil, lease.RunID}, {lease.TenantID, uuid.Nil}} {
		if operatorIdentityMatches(got, lease) {
			t.Fatal("foreign or missing identity accepted")
		}
	}
	if operatorIdentityMatches(exact, ApplyLease{}) {
		t.Fatal("unbound lease accepted")
	}
}

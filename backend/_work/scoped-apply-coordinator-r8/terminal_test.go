package main

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func TestStrict2655AndInstalledReceipt(t *testing.T) {
	s := syntheticScope()
	if e := s.validate(s.ObservedAt.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	s.Head = 2654
	if s.validate(s.ObservedAt.Add(time.Second)) == nil {
		t.Fatal("2654 accepted")
	}
	s.Head = 2655
	s.Main2655InstallSHA = strings.Repeat("a", 64)
	if s.validate(s.ObservedAt.Add(time.Second)) == nil {
		t.Fatal("unbound installation accepted")
	}
	if e := checkedMain2655(); e != nil {
		t.Fatal(e)
	}
}
func TestTerminalNormalizationProtectsTaskAndForeignQuota(t *testing.T) {
	s := syntheticScope()
	s.terminal = true
	s.Run = oldApplyRun
	task := map[string]json.RawMessage{"tenant_id": json.RawMessage(`"` + s.Tenant.String() + `"`), "id": json.RawMessage(`"` + oldFailedTask.String() + `"`), "state": json.RawMessage(`"failed"`)}
	skip, e := s.normalize("tasks", task)
	if e != nil || skip || text(task, "state") != "failed" {
		t.Fatal("terminal tasks must remain fully protected")
	}
	foreign := map[string]json.RawMessage{"tenant_id": json.RawMessage(`"` + uuid.New().String() + `"`), "holder_type": json.RawMessage(`"run"`), "holder_id": json.RawMessage(`"` + oldApplyRun.String() + `"`), "bytes": json.RawMessage(`128`)}
	s.normalizeTerminal("quota_reservations", foreign)
	if _, ok := foreign["bytes"]; !ok {
		t.Fatal("foreign quota normalized")
	}
}
func TestOldFailureGuardRefusesForeignBeforeQuery(t *testing.T) {
	s := syntheticScope()
	if oldFailureReady(context.Background(), nil, s) == nil || oldRecoveryReady(context.Background(), nil, s) == nil {
		t.Fatal("foreign scope admitted")
	}
}

// Records the early-failure branch's invariant: it must not invoke Inputs.
// Actual SQL and public failure algorithm remain covered by the genuine38case
// owned-clone qualification; this helper test does not claim live execution.
var _ pgx.Tx

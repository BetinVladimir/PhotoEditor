package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func TestStrict2655ScopeHeadAndInstallReceipt(t *testing.T) {
	now := time.Now()
	valid := Scope{Schema: 1, Head: 2655, Component: "planning", Approved: true, Jobs: make([]Job, 2), Observed: now, Main2655InstallReceiptSHA: strings.Repeat("a", 64)}
	if e := validateScopeEnvelope(valid, "planning", now); e != nil {
		t.Fatal(e)
	}
	for _, head := range []int64{0, 2654, 2656} {
		v := valid
		v.Head = head
		if validateScopeEnvelope(v, "planning", now) == nil {
			t.Fatal("wrong head accepted")
		}
	}
	for _, receipt := range []string{"", strings.Repeat("0", 64), strings.Repeat("A", 64), "notactual"} {
		v := valid
		v.Main2655InstallReceiptSHA = receipt
		if validateScopeEnvelope(v, "planning", now) == nil {
			t.Fatal("missing receipt binding accepted")
		}
	}
}

type headTx struct {
	pgx.Tx
	head  int64
	calls int
}

func (t *headTx) QueryRow(context.Context, string, ...any) pgx.Row {
	t.calls++
	return leaseRow{values: []any{t.head}}
}
func TestObservedWrongHeadRefusesBeforeAnyChildQueries(t *testing.T) {
	for _, head := range []int64{2654, 2656} {
		tx := &headTx{head: head}
		if observedTasks(context.Background(), tx, scope{}) == nil || tx.calls != 1 {
			t.Fatal("wrong head must refuse immediately")
		}
	}
}

package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestCapacitySchedulerContractIncludesNodeDrain(t *testing.T) {
	required := map[string]bool{
		"photo.begin_capacity_node_drain(text)":            false,
		"photo.capacity_node_drain_status(text,bigint)":    false,
		"photo.reap_capacity_node_drain_task(text,bigint)": false,
		"photo.capacity_pool_pending_backlog(text)":        false,
		"photo.capacity_pool_resource_usage(text,text)":    false,
	}
	for _, signature := range capacitySchedulerSignatures {
		if _, ok := required[signature]; ok {
			required[signature] = true
		}
	}
	for signature, present := range required {
		if !present {
			t.Fatal("missing scheduler drain contract", signature)
		}
	}
}

type capacityContractTx struct {
	pgx.Tx
	ready                         bool
	err                           error
	calls                         int
	signatures, privateSignatures []string
}

func (tx *capacityContractTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	tx.calls++
	if q != capacitySchedulerContractSQL || len(args) != 2 {
		return heavyAdmissionRow{err: errors.New("unexpected contract query")}
	}
	tx.signatures = args[0].([]string)
	tx.privateSignatures = args[1].([]string)
	return heavyAdmissionRow{allowed: tx.ready, err: tx.err}
}
func TestCapacitySchedulerContractRefusesIncompleteInstall(t *testing.T) {
	for _, ready := range []bool{false, true} {
		tx := &capacityContractTx{ready: ready}
		err := CheckCapacitySchedulerContract(context.Background(), tx)
		if (err == nil) != ready || tx.calls != 1 || len(tx.signatures) != len(capacitySchedulerSignatures) || len(tx.privateSignatures) != len(capacitySchedulerPrivateSignatures) {
			t.Fatal("wrong readiness", err)
		}
		tx.signatures[0] = "changed"
		if capacitySchedulerSignatures[0] == "changed" {
			t.Fatal("caller modified contract inventory")
		}
		tx.privateSignatures[0] = "changed"
		if capacitySchedulerPrivateSignatures[0] == "changed" {
			t.Fatal("caller modified private contract inventory")
		}
	}
	sentinel := errors.New("catalog query failed")
	tx := &capacityContractTx{err: sentinel}
	if err := CheckCapacitySchedulerContract(context.Background(), tx); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx.calls = 0
	if err := CheckCapacitySchedulerContract(ctx, tx); !errors.Is(err, context.Canceled) || tx.calls != 0 {
		t.Fatal("cancel queried catalog", err)
	}
	if CheckCapacitySchedulerContract(context.Background(), nil) == nil {
		t.Fatal("nil transaction accepted")
	}
}

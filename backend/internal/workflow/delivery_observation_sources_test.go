package workflow

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
)

type ordinaryPendingTx struct {
	watermarkParamsTx
	raw []byte
}

func (tx *ordinaryPendingTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if query != `SELECT photo.v30_delivery_pending_child_slots($1,$2,$3)` {
		panic("unexpected pending query")
	}
	tx.events = append(tx.events, "query")
	return deliveryActivationRow{value: tx.raw}
}

type ordinaryPendingPool struct{ tx *ordinaryPendingTx }

func (pool ordinaryPendingPool) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return pool.tx, nil
}

func TestOrdinaryDeliveryDiscoverySetsTenantBeforeQuery(t *testing.T) {
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(), SlotCount: 2, Cursor: 2}
	for _, scenario := range []struct {
		raw   string
		valid bool
	}{{"[0,1]", true}, {"[1,0]", false}, {"null", false}} {
		tx := &ordinaryPendingTx{raw: []byte(scenario.raw)}
		slots, err := readOrdinaryDeliveryPendingSlots(context.Background(), ordinaryPendingPool{tx}, lease, 2)
		want := []string{"tenant", "query", "rollback"}
		if scenario.valid {
			want = []string{"tenant", "query", "commit", "rollback"}
		}
		if (err == nil) != scenario.valid || !reflect.DeepEqual(tx.events, want) || !scenario.valid && slots != nil {
			t.Fatal(scenario, slots, err, tx.events)
		}
	}
}

func TestDeliveryPendingSlotsRejectsUnscopedOrUnorderedCandidates(t *testing.T) {
	for _, scenario := range []struct {
		raw string
		bad bool
	}{{"[]", false}, {"[0,2,3]", false}, {"null", true}, {"[2,1]", true}, {"[1,1]", true}, {"[-1]", true}, {"[4]", true}, {"[0,1,2,3]", true}, {"[1.5]", true}, {"{}", true}} {
		slots, err := parseDeliveryPendingSlots([]byte(scenario.raw), 4, 3)
		if (err != nil) != scenario.bad {
			t.Fatal(scenario.raw, slots, err)
		}
	}
}

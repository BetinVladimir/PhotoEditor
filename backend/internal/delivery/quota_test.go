package delivery

import (
	"context"
	"testing"
)

func TestReleaseNextDeliveryQuotaPropagatesSettlement(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		db := &assemblyRetainDB{accepted: accepted}
		released, err := (ExpansionLeaseStore{DB: db}).ReleaseNextQuota(context.Background())
		if err != nil || released != accepted || db.calls != 1 || len(db.args) != 0 {
			t.Fatal(released, err, db)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &assemblyRetainDB{}
	if _, err := (ExpansionLeaseStore{DB: db}).ReleaseNextQuota(ctx); err == nil || db.calls != 0 {
		t.Fatal("cancelled cleanup reached SQL")
	}
	if _, err := (ExpansionLeaseStore{}).ReleaseNextQuota(context.Background()); err == nil {
		t.Fatal("nil store accepted")
	}
}

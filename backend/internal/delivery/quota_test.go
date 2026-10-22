package delivery

import (
	"context"
	"testing"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
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


type deliverySourceCleanupDB struct { queries []string; source, settlement bool; failSettlement bool }
func (d *deliverySourceCleanupDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
 d.queries = append(d.queries, query)
 if len(d.queries) == 1 { return assemblyRetainRow{accepted:d.source} }
 if d.failSettlement { return assemblyRetainRow{err:errors.New("settlement unavailable")} }
 return assemblyRetainRow{accepted:d.settlement}
}
func TestDeliveryCleanupPreservesCommittedSourceFailureProgress(t *testing.T) {
 for _, scenario := range []string{"source", "settlement", "idle", "settlement_error"} {
  db := &deliverySourceCleanupDB{source:scenario=="source"||scenario=="settlement_error",settlement:scenario=="settlement",failSettlement:scenario=="settlement_error"}
  changed, err := (ExpansionLeaseStore{DB:db,Instance:uuid.New(),Bucket:"photo"}).ReleaseNextQuota(context.Background())
  if changed != (scenario!="idle") || (err!=nil)!=(scenario=="settlement_error") { t.Fatal(scenario,changed,err) }
  if len(db.queries)!=2 || db.queries[0]!=`SELECT photo.fail_next_v30_delivery_missing_source($1,$2)` || db.queries[1]!=`SELECT photo.cleanup_next_v30_delivery()` { t.Fatal("cleanup order",db.queries) }
 }
}

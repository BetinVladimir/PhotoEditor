package v2draft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type glareMaintenanceRow struct { changed bool; err error }
func (r glareMaintenanceRow) Scan(out ...any) error { if r.err != nil { return r.err }; *out[0].(*bool)=r.changed; return nil }
type glareMaintenanceDB struct { queries []string; rows []glareMaintenanceRow; cancel context.CancelFunc }
func (d *glareMaintenanceDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	d.queries=append(d.queries,query)
	i:=len(d.queries)-1
	if d.cancel!=nil { d.cancel() }
	return d.rows[i]
}

func TestGlareWithdrawalUsesSQLWitnessAndSettlesSeparately(t *testing.T) {
	for _, tc := range []struct { name string; rows []glareMaintenanceRow; changed bool; calls int; failed bool }{
		{"withdraw",[]glareMaintenanceRow{{changed:true}},true,1,false},
		{"settle_after_drain",[]glareMaintenanceRow{{},{changed:true}},true,2,false},
		{"no_permanent_witness",[]glareMaintenanceRow{{},{}},false,2,false},
		{"database_failure",[]glareMaintenanceRow{{err:errors.New("temporary SQL failure")}},false,1,true},
	} {
		t.Run(tc.name,func(t *testing.T){
			db:=&glareMaintenanceDB{rows:tc.rows}
			changed,err:=(SQLGlareAdoptionWithdrawal{DB:db}).ProcessOne(context.Background())
			if changed!=tc.changed || (err!=nil)!=tc.failed || len(db.queries)!=tc.calls { t.Fatalf("changed=%v err=%v calls=%d",changed,err,len(db.queries)) }
			if db.queries[0]!="SELECT photo.withdraw_completed_glare_consent()" { t.Fatal("not SQL-owned revocation classification") }
			if tc.calls==2 && db.queries[1]!="SELECT photo.release_next_glare_withdrawn_quota()" { t.Fatal("missing independent settlement") }
		})
	}
}

func TestGlareWithdrawalLateCallbackCannotReportSuccess(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	db:=&glareMaintenanceDB{rows:[]glareMaintenanceRow{{changed:true}},cancel:cancel}
	changed,err:=(SQLGlareAdoptionWithdrawal{DB:db}).ProcessOne(ctx)
	if changed || !errors.Is(err,context.Canceled) { t.Fatal("late maintenance callback escaped cancellation",err) }
}

func TestGlareMaintenanceRunsBeforePhysicalAdoption(t *testing.T) {
	db:=&glareMaintenanceDB{rows:[]glareMaintenanceRow{{changed:true}}}
	adopter:=&restorationCandidateAdopterFixture{}
	dispatcher:=RestorationAdoptionDispatcher{Candidates:restorationCandidateQueueFixture{},Adopter:adopter,
	 Withdrawal:SQLGlareAdoptionWithdrawal{DB:db},Timeout:time.Minute}
	changed,err:=dispatcher.ProcessOne(context.Background())
	if err!=nil || !changed || adopter.calls!=0 { t.Fatal("withdrawn task reached physical adoption",err) }
}

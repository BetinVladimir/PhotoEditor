package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type consentMaintenanceRow struct { value bool; err error }
func (r consentMaintenanceRow) Scan(out ...any) error { if r.err!=nil { return r.err };*out[0].(*bool)=r.value;return nil }
type consentMaintenanceFixture struct { rows []consentMaintenanceRow;queries []string;bounded bool;cancel context.CancelFunc }
func (d *consentMaintenanceFixture) QueryRow(ctx context.Context, query string,_ ...any) pgx.Row {
	_,deadline:=ctx.Deadline();d.bounded=deadline;d.queries=append(d.queries,query)
	if d.cancel!=nil { d.cancel() }
	return d.rows[len(d.queries)-1]
}

func TestConsentRecoveryPassIsIndependentBoundedAndOrdered(t *testing.T) {
	queries:=[]string{"SELECT photo.withdraw_completed_glare_consent()","SELECT photo.withdraw_completed_open_eyes_consent()"}
	db:=&consentMaintenanceFixture{rows:[]consentMaintenanceRow{{value:true},{},{value:true},{}}}
	count,err:=processConsentMaintenance(context.Background(),db,queries,3)
	if err!=nil || count!=2 || !db.bounded || len(db.queries)!=4 { t.Fatal("recovery pass missing",count,err,db.queries) }
	if db.queries[0]!=queries[0] || db.queries[1]!=queries[0] || db.queries[2]!=queries[1] || db.queries[3]!=queries[1] { t.Fatal("independent pass order changed") }
}

func TestConsentRecoveryInfrastructureFailureDoesNotBecomeWithdrawal(t *testing.T) {
	failure:=errors.New("temporary database failure")
	db:=&consentMaintenanceFixture{rows:[]consentMaintenanceRow{{err:failure}}}
	count,err:=processConsentMaintenance(context.Background(),db,[]string{"withdraw","settle"},2)
	if count!=0 || !errors.Is(err,failure) || len(db.queries)!=1 { t.Fatal("infrastructure classified permanent",count,err) }
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	db=&consentMaintenanceFixture{rows:[]consentMaintenanceRow{{value:true}},cancel:cancel}
	count,err=processConsentMaintenance(ctx,db,[]string{"withdraw"},1)
	if count!=0 || !errors.Is(err,context.Canceled) { t.Fatal("late callback escaped context",count,err) }
}

func TestConsentRecoveryPassBoundsNumberOfWrites(t *testing.T) {
	db:=&consentMaintenanceFixture{rows:[]consentMaintenanceRow{{value:true},{value:true}}}
	count,err:=processConsentMaintenance(context.Background(),db,[]string{"withdraw"},2)
	if count!=2 || err!=nil || len(db.queries)!=2 { t.Fatal("unbounded pass",count,err) }
}

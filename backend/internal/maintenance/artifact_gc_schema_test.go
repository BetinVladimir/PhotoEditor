package maintenance

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)

type gcAccountingRow struct { ready bool; err error; after func() }
func (r gcAccountingRow) Scan(dest ...any) error {
 if r.after!=nil {r.after()}
 if r.err!=nil {return r.err}
 *(dest[0].(*bool))=r.ready
 return nil
}
type gcAccountingDB struct {row gcAccountingRow; calls int}
func (d *gcAccountingDB) QueryRow(context.Context,string,...any) pgx.Row {d.calls++;return d.row}

func TestArtifactGCAccountingRefusesMissingSchema(t *testing.T){
 db:=&gcAccountingDB{}
 if RequireArtifactGCAccounting(context.Background(),db)==nil {t.Fatal("missing schema accepted")}
 db.row.ready=true
 if err:=RequireArtifactGCAccounting(context.Background(),db);err!=nil {t.Fatal(err)}
}
func TestArtifactGCAccountingHonorsCancellationAroundProbe(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 db:=&gcAccountingDB{row:gcAccountingRow{ready:true}}
 if !errors.Is(RequireArtifactGCAccounting(ctx,db),context.Canceled)||db.calls!=0 {t.Fatal("cancelled startup queried schema")}
 ctx,cancel=context.WithCancel(context.Background());defer cancel()
 db.row.after=cancel
 if !errors.Is(RequireArtifactGCAccounting(ctx,db),context.Canceled) {t.Fatal("late successful probe ignored cancellation")}
}
func TestArtifactGCAccountingPropagatesProbeFailure(t *testing.T){
 expected:=errors.New("database unavailable")
 db:=&gcAccountingDB{row:gcAccountingRow{err:expected}}
 if !errors.Is(RequireArtifactGCAccounting(context.Background(),db),expected) {t.Fatal("probe failure hidden")}
}

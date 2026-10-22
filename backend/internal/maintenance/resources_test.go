package maintenance

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
type withdrawalRow struct{ids []uuid.UUID;kind string;value bool;err error}
func(r withdrawalRow)Scan(d ...any)error{if r.err!=nil{return r.err};if len(d)==1{*d[0].(*bool)=r.value;return nil};*d[0].(*uuid.UUID)=r.ids[0];*d[1].(*uuid.UUID)=r.ids[1];*d[2].(*uuid.UUID)=r.ids[2];*d[3].(*string)=r.kind;*d[4].(*uuid.UUID)=r.ids[3];return nil}
type withdrawalDB struct{rows []withdrawalRow;calls int;args []any;cancel context.CancelFunc}
func(d *withdrawalDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{r:=d.rows[d.calls];d.calls++;d.args=args;if d.cancel!=nil{d.cancel()};return r}
func TestResourceWithdrawalExactClaimAndRetry(t *testing.T){
 ids:=[]uuid.UUID{uuid.New(),uuid.New(),uuid.New(),uuid.New()}
 for _,kind:=range []string{"project","asset"}{for _,done:=range []bool{false,true}{db:=&withdrawalDB{rows:[]withdrawalRow{{ids:ids,kind:kind},{value:done}}};got,err:=(ResourceWithdrawalCleaner{DB:db}).CleanOne(context.Background());if err!=nil||got!=done||db.calls!=2||db.args[0]!=ids[2]||db.args[1]!=ids[0]||db.args[2]!=kind||db.args[3]!=ids[3]||db.args[4]!=ids[1]{t.Fatal("claim authority changed",got,err,db.args)}}}
 for _,kind:=range []string{"tenant","collection",""}{db:=&withdrawalDB{rows:[]withdrawalRow{{ids:ids,kind:kind}}};if _,err:=(ResourceWithdrawalCleaner{DB:db}).CleanOne(context.Background());err==nil||db.calls!=1{t.Fatal("unsupported resource finalized")}}
 ctx,cancel:=context.WithCancel(context.Background());db:=&withdrawalDB{rows:[]withdrawalRow{{ids:ids,kind:"asset"}},cancel:cancel};if _,err:=(ResourceWithdrawalCleaner{DB:db}).CleanOne(ctx);!errors.Is(err,context.Canceled)||db.calls!=1{t.Fatal("cancelled claim finalized",err)}
}
func TestResourceWithdrawalInstallationBoundedAndFailClosed(t *testing.T){
 for _,ready:=range []bool{false,true}{db:=&collectionInstallationDB{ready:ready};err:=RequireResourceWithdrawal(context.Background(),db);if (err==nil)!=ready||!db.bounded||db.calls!=1{t.Fatal("installation bypass",err)}}
 ctx,cancel:=context.WithCancel(context.Background());db:=&collectionInstallationDB{ready:true,after:cancel};if err:=RequireResourceWithdrawal(ctx,db);!errors.Is(err,context.Canceled){t.Fatal("late readiness published",err)}
}

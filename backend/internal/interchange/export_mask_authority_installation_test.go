package interchange

import (
 "context"
 "errors"
 "os"
 "testing"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
type exportMaskInstallationRow struct{installed bool;err error}
func(r exportMaskInstallationRow)Scan(dest ...any)error{if r.err!=nil{return r.err};*dest[0].(*bool)=r.installed;return nil}
type exportMaskInstallationQuery struct{calls int;row exportMaskInstallationRow;cancel context.CancelFunc}
func(q *exportMaskInstallationQuery)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{
 q.calls++;if q.cancel!=nil{q.cancel()};return q.row
}
func TestExportMaskAuthorityInstallationRefusesMissingOrCancelled(t *testing.T){
 cause:=errors.New("catalog unavailable")
 for _,mode:=range []string{"valid","missing","query_error","cancelled","cancel_during_query"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  query:=&exportMaskInstallationQuery{row:exportMaskInstallationRow{installed:true}}
  switch mode{case "missing":query.row.installed=false;case "query_error":query.row.err=cause;case "cancelled":cancel();case "cancel_during_query":query.cancel=cancel}
  err:=validateExportMaskAuthorityInstallation(ctx,query)
  switch mode{case "valid":if err!=nil||query.calls!=1{t.Fatal(err)};case "missing":if err!=v2input.ErrInvalid{t.Fatal(err)};case "query_error":if !errors.Is(err,cause){t.Fatal(err)};default:if err!=context.Canceled{t.Fatal(err)}}
  if mode=="cancelled"&&query.calls!=0{t.Fatal("cancelled preflight reached database")}
 })}
 if err:=validateExportMaskAuthorityInstallation(context.Background(),nil);err!=v2input.ErrInvalid{t.Fatal(err)}
}
func TestIntegrationExportMaskAuthorityInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database with migrations01564–01565 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 if err:=validateExportMaskAuthorityInstallation(ctx,db);err!=nil{t.Fatal(err)}
}

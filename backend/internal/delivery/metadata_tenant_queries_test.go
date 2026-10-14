package delivery

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

type metadataQueryTx struct {pgx.Tx;events []string;commitErr error;scanErr error;cancel context.CancelFunc}
func(t *metadataQueryTx)Exec(_ context.Context,q string,args ...any)(pgconn.CommandTag,error){
 if q!=`SELECT set_config('app.tenant_id',$1,true)`{panic("unexpected tenant setup")}
 t.events=append(t.events,"tenant");return pgconn.NewCommandTag("SELECT 1"),nil
}
func(t *metadataQueryTx)QueryRow(context.Context,string,...any)pgx.Row{
 t.events=append(t.events,"query")
 return metadataStorageRow{func(dest []any)error{
  if t.cancel!=nil{t.cancel()}
  if t.scanErr!=nil{return t.scanErr}
  *dest[0].(*bool)=true;return nil
 }}
}
func(t *metadataQueryTx)Commit(context.Context)error{t.events=append(t.events,"commit");return t.commitErr}
func(t *metadataQueryTx)Rollback(context.Context)error{t.events=append(t.events,"rollback");return nil}
type metadataQueryPool struct {tx *metadataQueryTx;begins int}
func(p *metadataQueryPool)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){p.begins++;return p.tx,nil}

func TestMetadataTenantQueriesCommitBeforeReturning(t *testing.T){
 for _,mode:=range []string{"valid","scan","commit","cancel"}{t.Run(mode,func(t *testing.T){
  tenant:=uuid.New();tx:=&metadataQueryTx{};pool:=&metadataQueryPool{tx:tx};ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  if mode=="scan"{tx.scanErr=errors.New("scan")};if mode=="commit"{tx.commitErr=errors.New("commit")};if mode=="cancel"{tx.cancel=cancel}
  query:=MetadataTenantQueries{DB:pool,Tenant:tenant};var accepted bool
  err:=query.QueryRow(ctx,`SELECT photo.pin_v30_delivery_metadata_proof($1,$2,$3,$4,$5)`,tenant,uuid.New(),uuid.New(),"{}","a").Scan(&accepted)
  if (err==nil)!=(mode=="valid"){t.Fatal(mode,err)}
  if pool.begins!=1||tx.events[0]!="tenant"||tx.events[1]!="query"||tx.events[len(tx.events)-1]!="rollback"{t.Fatal(tx.events)}
  if mode=="valid"&&(len(tx.events)!=4||tx.events[2]!="commit"||!accepted){t.Fatal("transaction remained open",tx.events)}
 })}
}
func TestMetadataTenantQueriesRefuseForeignScopeAndArbitrarySQL(t *testing.T){
 tenant:=uuid.New();pool:=&metadataQueryPool{tx:&metadataQueryTx{}};query:=MetadataTenantQueries{DB:pool,Tenant:tenant};var accepted bool
 for _,input:=range []struct{q string;tenant uuid.UUID}{{"SELECT true",tenant},{`SELECT photo.pin_v30_delivery_metadata_proof($1,$2,$3,$4,$5)`,uuid.New()}}{
  if err:=query.QueryRow(context.Background(),input.q,input.tenant).Scan(&accepted);err==nil||pool.begins!=0{t.Fatal("scope/query bypass")}
 }
}

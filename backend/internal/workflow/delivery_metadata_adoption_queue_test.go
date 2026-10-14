package workflow

import (
 "context"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/delivery"
)

type metadataSettlementDB struct{delivery.MetadataSchedulerDB;tx *metadataSettlementTx;begins int}
func(d *metadataSettlementDB)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){d.begins++;return d.tx,nil}
type metadataSettlementTx struct{pgx.Tx;queries []string;arguments [][]any;state string;commits int}
func(t *metadataSettlementTx)Exec(_ context.Context,q string,args ...any)(pgconn.CommandTag,error){
 t.queries=append(t.queries,q);t.arguments=append(t.arguments,args);return pgconn.NewCommandTag("SELECT 1"),nil
}
func(t *metadataSettlementTx)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 t.queries=append(t.queries,q);t.arguments=append(t.arguments,args);return metadataSettlementRow{t.state}
}
func(t *metadataSettlementTx)Commit(context.Context)error{t.commits++;return nil}
func(t *metadataSettlementTx)Rollback(context.Context)error{return nil}
type metadataSettlementRow struct{state string}
func(r metadataSettlementRow)Scan(dest ...any)error{*dest[0].(*string)=r.state;return nil}

func TestMetadataSettlementOwnsExactCurrentScope(t *testing.T){
 candidate:=MetadataAdoptionCandidate{Tenant:uuid.New(),Task:uuid.New(),Result:uuid.New(),Attempt:2,Fence:7}
 for _,state:=range []string{"deferred","rejected","stale"}{t.Run(state,func(t *testing.T){
  tx:=&metadataSettlementTx{state:state};db:=&metadataSettlementDB{tx:tx}
  got,err:=(MetadataAdoptionQueue{DB:db}).SettleInvalid(context.Background(),candidate,"DELIVERY_METADATA_RASTER_BYTES")
  if err!=nil||got!=state||db.begins!=1||tx.commits!=1||len(tx.queries)!=2{t.Fatal("settlement failed",state,got,err)}
  if tx.queries[0]!="SELECT set_config('app.tenant_id',$1,true)"||len(tx.arguments[0])!=1||tx.arguments[0][0]!=candidate.Tenant.String(){t.Fatal("tenant scope changed")}
  if tx.queries[1]!=`SELECT photo.settle_v30_metadata_invalid($1,$2,$3,$4,$5,$6)`||len(tx.arguments[1])!=6{
   t.Fatal("settlement ABI changed")
  }
  expected:=[]any{candidate.Tenant,candidate.Task,candidate.Attempt,candidate.Fence,candidate.Result,"DELIVERY_METADATA_RASTER_BYTES"}
  for i,value:=range expected{if tx.arguments[1][i]!=value{t.Fatal("settlement scope changed",i)}}
 })}
 tx:=&metadataSettlementTx{state:"rejected"};db:=&metadataSettlementDB{tx:tx}
 if _,err:=(MetadataAdoptionQueue{DB:db}).SettleInvalid(context.Background(),candidate,"DELIVERY_METADATA_AUTHORITY_MISSING");err==nil||db.begins!=0{t.Fatal("authority failure reached SQL")}
 cancelled,stop:=context.WithCancel(context.Background());stop()
 if _,err:=(MetadataAdoptionQueue{DB:db}).SettleInvalid(cancelled,candidate,"DELIVERY_METADATA_RASTER_BYTES");err!=context.Canceled||db.begins!=0{t.Fatal("cancelled settlement reached SQL",err)}
}

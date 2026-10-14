package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
)
type watermarkParamsTx struct{pgx.Tx;events []string;query string;row deliveryParamsRow}
func(t *watermarkParamsTx)Exec(_ context.Context,q string,_ ...any)(pgconn.CommandTag,error){if q!=`SELECT set_config('app.tenant_id',$1,true)`{panic("tenant setup")};t.events=append(t.events,"tenant");return pgconn.NewCommandTag("SELECT 1"),nil}
func(t *watermarkParamsTx)QueryRow(_ context.Context,q string,_ ...any)pgx.Row{t.query=q;t.events=append(t.events,"query");return t.row}
func(t *watermarkParamsTx)Commit(context.Context)error{t.events=append(t.events,"commit");return nil}
func(t *watermarkParamsTx)Rollback(context.Context)error{t.events=append(t.events,"rollback");return nil}
type watermarkParamsPool struct{tx *watermarkParamsTx;begins int}
func(p *watermarkParamsPool)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){p.begins++;return p.tx,nil}
func TestWatermarkParamsAdapterCommitsAndRestrictsQueries(t *testing.T){
 tenant:=uuid.New();tx:=&watermarkParamsTx{row:deliveryParamsRow{outcome:"created",id:uuid.New(),token:uuid.New()}};pool:=&watermarkParamsPool{tx:tx}
 lease:=delivery.ExpansionLease{Tenant:tenant,Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),SlotCount:1,Cursor:0}
 repository:=deliveryParamsRepository{db:watermarkEncodeParamsQueries{db:pool,tenant:tenant},bucket:"photos",lease:lease,owner:uuid.New(),params:[]byte(`{"image":{}}`)}
 if _,err:=repository.Reserve(context.Background(),repository.scope(),renderDerivativeBytesSHA(repository.params),int64(len(repository.params)));err!=nil{t.Fatal(err)}
 if tx.query!=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_watermark_encode_params($1,$2,$3,$4,$5,$6,$7,$8)`||len(tx.events)!=4||tx.events[2]!="commit"{t.Fatal("transaction/query isolation",tx.events,tx.query)}
 adapter:=watermarkEncodeParamsQueries{db:pool,tenant:tenant};before:=pool.begins;var accepted bool
 if err:=adapter.QueryRow(context.Background(),"SELECT true",tenant).Scan(&accepted);err==nil||pool.begins!=before{t.Fatal("arbitrary SQL accepted")}
 foreign:=artifactjson.Scope{Tenant:uuid.New()}
 if _,err:=repository.Reserve(context.Background(),foreign,"invalid",0);err==nil||pool.begins!=before{t.Fatal("foreign scope accepted")}
}

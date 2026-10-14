package workflow

import (
 "context"
 "errors"
 "reflect"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
type assemblyQueryLifecycleTx struct {
 watermarkParamsTx
 tenant uuid.UUID
 query string
 args []any
 queryFailure,commitFailure error
}
func(tx *assemblyQueryLifecycleTx)Exec(ctx context.Context,q string,args ...any)(pgconn.CommandTag,error){
 if len(args)!=1||args[0]!=tx.tenant.String(){panic("incorrect assembly tenant")}
 return tx.watermarkParamsTx.Exec(ctx,q,args...)
}
func(tx *assemblyQueryLifecycleTx)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 tx.events=append(tx.events,"query");tx.query=q;tx.args=append([]any(nil),args...)
 if tx.queryFailure!=nil{return watermarkSourceQueryRow{failure:tx.queryFailure}}
 return deliveryActivationRow{value:[]byte("result")}
}
func(tx *assemblyQueryLifecycleTx)Commit(context.Context)error{tx.events=append(tx.events,"commit");return tx.commitFailure}
type assemblyQueryLifecycleDB struct{tx *assemblyQueryLifecycleTx}
func(db assemblyQueryLifecycleDB)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){return db.tx,nil}
func TestAssemblyQueryAdaptersTenantCommitAndErrorPropagation(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:501,Cursor:501}
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 task,err:=v2input.TaskID(l.Workflow,"v2.delivery.assemble",0);if err!=nil{t.Fatal(err)}
 for _,kind:=range []string{"observation","recovery","publication","transport"}{
  for _,phase:=range []string{"success","query failure","commit failure"}{t.Run(kind+"/"+phase,func(t *testing.T){
   failure:=errors.New(phase);tx:=&assemblyQueryLifecycleTx{tenant:l.Tenant}
   if phase=="query failure"{tx.queryFailure=failure};if phase=="commit failure"{tx.commitFailure=failure}
   db:=assemblyQueryLifecycleDB{tx};var adapter artifactjson.RowQuerier;var query string;var args []any
   switch kind{
   case "observation":adapter=deliveryAssemblyObservationQueries{db:db,lease:l};query=`SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`;args=[]any{l.Expansion,l.Token,0,100}
   case "recovery":adapter=deliveryAssemblyRecoveryQueries{deliveryAssemblyObservationQueries:deliveryAssemblyObservationQueries{db:db,lease:l},root:root};query=`SELECT photo.v30_delivery_saved_shard($1,$2,$3,$4,$5)`;args=[]any{l.Expansion,l.Token,root.ArtifactID,root.SHA256,1}
   case "publication":adapter=deliveryAssemblyPublicationQueries{db:db,lease:l};query=`SELECT photo.retain_v30_delivery_assembly($1,$2,$3,$4)`;args=[]any{l.Expansion,l.Token,root.ArtifactID,root.SHA256}
   case "transport":adapter=deliveryAssemblyTransportQueries{db:db,lease:l,task:task};query="SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document($1,$2,$3,$4,$5,$6,$7)";args=[]any{l.Tenant,l.Run,task,"v2_input_chunk_0",root.SHA256,int64(2),"documents"}
   }
   var raw []byte;err:=adapter.QueryRow(context.Background(),query,args...).Scan(&raw)
   want:=[]string{"tenant","query","commit","rollback"};if phase=="query failure"{want=[]string{"tenant","query","rollback"}}
   if !reflect.DeepEqual(tx.events,want)||tx.query!=query||!reflect.DeepEqual(tx.args,args){t.Fatal(tx.events,tx.query,tx.args)}
   if phase=="success"{if err!=nil||string(raw)!="result"{t.Fatal(err,string(raw))}}else if !errors.Is(err,failure){t.Fatal("failure lost",err)}
  })}
 }
}

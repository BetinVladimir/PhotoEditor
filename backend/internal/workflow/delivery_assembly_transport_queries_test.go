package workflow
import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestAssemblyTransportRefusesForeignTaskAndPurposeBeforeSQL(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:1}
 task,err:=v2input.TaskID(l.Workflow,"v2.delivery.assemble",0);if err!=nil{t.Fatal(err)}
 query:="SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document($1,$2,$3,$4,$5,$6,$7)"
 for _,purpose:=range []string{"task_params","v2_input_chunk_-1","v2_input_chunk_01","v2_input_chunk_1954","v2_input_chunk_"}{
  db:=&runtimePlanUnexpectedDB{};q:=deliveryAssemblyTransportQueries{db:db,lease:l,task:task}
  if err:=q.QueryRow(context.Background(),query,l.Tenant,l.Run,task,purpose,"sha",int64(2),"documents").Scan();err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(purpose,err,db.calls)}
 }
 db:=&runtimePlanUnexpectedDB{};q:=deliveryAssemblyTransportQueries{db:db,lease:l,task:task}
 if err:=q.QueryRow(context.Background(),query,l.Tenant,l.Run,uuid.New(),"v2_input_manifest","sha",int64(2),"documents").Scan();err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(err,db.calls)}
}

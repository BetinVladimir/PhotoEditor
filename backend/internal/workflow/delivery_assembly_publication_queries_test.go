package workflow

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestAssemblyPublicationRejectsForeignScopeBeforeTransaction(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:1,Cursor:1}
 query:=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_assembly_params($1,$2,$3,$4,$5,$6,$7)`
 for _,index:=range []int{0,1,4,5,6}{
  args:=[]any{l.Expansion,l.Token,"{}","documents",l.Tenant,l.Run,uuid.New()}
  if index==6{args[index]=uuid.Nil}else{args[index]=uuid.New()}
  db:=&runtimePlanUnexpectedDB{};q:=deliveryAssemblyPublicationQueries{db:db,lease:l}
  if err:=q.QueryRow(context.Background(),query,args...).Scan();err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(index,err,db.calls)}
 }
 db:=&runtimePlanUnexpectedDB{};q:=deliveryAssemblyPublicationQueries{db:db,lease:l}
 if err:=q.QueryRow(context.Background(),"SELECT photo.complete_backend_document($1)",l.Tenant).Scan();err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(err,db.calls)}
}

package workflow

import (
 "context"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestAssemblyRecoveryRejectsRootAndLeaseSubstitutionBeforeDatabase(t *testing.T){
 l:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now(),Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),SlotCount:501,Cursor:501}
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 for _,test:=range []struct{query string;args []any}{
  {`SELECT photo.v30_delivery_saved_assembly($1,$2,$3,$4)`,[]any{uuid.New(),l.Token,root.ArtifactID,root.SHA256}},
  {`SELECT photo.v30_delivery_saved_assembly($1,$2,$3,$4)`,[]any{l.Expansion,uuid.New(),root.ArtifactID,root.SHA256}},
  {`SELECT photo.v30_delivery_assembly_inventory($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,uuid.New(),root.SHA256}},
  {`SELECT photo.v30_delivery_saved_assembly($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,root.ArtifactID,strings.Repeat("b",64)}},
  {`SELECT photo.v30_delivery_saved_shard($1,$2,$3,$4,$5)`,[]any{l.Expansion,l.Token,root.ArtifactID,root.SHA256,-1}},
  {`SELECT photo.v30_delivery_saved_shard($1,$2,$3,$4,$5)`,[]any{l.Expansion,l.Token,root.ArtifactID,root.SHA256,2}},
  {`SELECT photo.retain_v30_delivery_assembly($1,$2,$3,$4)`,[]any{l.Expansion,l.Token,root.ArtifactID,root.SHA256}},
 }{
  db:=&runtimePlanUnexpectedDB{}
  q:=deliveryAssemblyRecoveryQueries{deliveryAssemblyObservationQueries:deliveryAssemblyObservationQueries{db:db,lease:l},root:root}
  if err:=q.QueryRow(context.Background(),test.query,test.args...).Scan();err!=v2input.ErrInvalid||db.calls!=0{t.Fatal(test.query,err,db.calls)}
 }
}

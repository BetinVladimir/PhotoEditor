package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorAdoptionLoaderRequiresCurrentSourceReader(t *testing.T){
 d,_:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 loader:=ReplacementDetectionAdoptionLoader{DB:db,Bucket:"photos"}
 candidate:=ReplacementDetectionAdoptionCandidate{Tenant:d.Tenant,Run:d.Run,Task:d.Task,Snapshot:uuid.New(),Descriptor:raw}
 if _,err=loader.Load(context.Background(),candidate);err!=v2input.ErrInvalid{t.Fatal("missing physical reader accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=loader.Load(ctx,candidate);err!=context.Canceled{t.Fatal("cancelled proof reload accepted",err)}
 if db.calls!=0{t.Fatal("invalid loader reached database")}
}

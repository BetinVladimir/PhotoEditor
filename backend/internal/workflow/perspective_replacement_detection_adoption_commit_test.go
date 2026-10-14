package workflow
import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorCommitRequiresCallerTransaction(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 if err=r.CommitAdoption(context.Background(),nil,1,1,uuid.New(),[]byte(`{}`),strings.Repeat("a",64),[]byte(`{}`),[]byte(`{}`));err!=v2input.ErrInvalid{t.Fatal("commit without caller transaction accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=r.CommitAdoption(ctx,nil,1,1,uuid.New(),nil,"",nil,nil);err!=context.Canceled{t.Fatal("cancelled commit accepted",err)}
 if db.calls!=0{t.Fatal("commit opened independent transaction")}
}

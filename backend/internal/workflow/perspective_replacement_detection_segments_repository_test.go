package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
)
func TestReplacementSegmentsRepositoryRejectsForeignPacketBeforeDB(t *testing.T){
 db:=&detectionDocumentDB{}
 scope:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"task_params"}
 plan:=ReplacementDetectionSegmentsPlan{scope:scope,artifact:uuid.New(),body:"{}",sha:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 repo:=replacementSegmentsRepository{source:ReplacementDetectionDocuments{DB:db},plan:plan}
 foreign:=scope;foreign.Owner=uuid.New()
 if _,err:=repo.Reserve(context.Background(),foreign,plan.sha,2);err==nil{t.Fatal("foreign owner reserved")}
 if _,err:=repo.Reserve(context.Background(),scope,plan.sha,3);err==nil{t.Fatal("wrong size reserved")}
 if _,err:=repo.Complete(context.Background(),scope,artifactjson.Reservation{ArtifactID:plan.artifact,Token:uuid.New()},objectstore.Object{SHA256:plan.sha,Bytes:2,Key:"foreign",ContentType:"application/json",VersionID:"v1"});err==nil{t.Fatal("foreign key completed")}
 if db.calls!=0{t.Fatal("invalid packet reached database")}
 if _,err:=(ReplacementDetectionDocuments{}).PublishSegments(context.Background(),nil);err==nil{t.Fatal("missing object store accepted")}
}

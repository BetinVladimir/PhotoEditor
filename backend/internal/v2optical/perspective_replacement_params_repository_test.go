package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2qualification"
)

// These rejection cases do not substitute for a qualified positive publication
// fixture covering quota, fencing, ambiguous PUT and admission recovery.
func TestManualPerspectiveReplacementParamsRejectIncompletePackageBeforeSQLAndIO(t *testing.T){
 db:=&negativeDraftStoreDB{}
 objects:=&negativeParamsObjects{}
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 ref,err:=prepared.PublishManualPerspectiveReplacementParams(context.Background(),db,objects,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{raw:"{}"},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{})
 if err==nil||ref.ID!=uuid.Nil||db.begins!=0||objects.puts!=0||objects.recoveries!=0{t.Fatal("incomplete package reached publication",err)}
 repo:=manualPerspectiveReplacementParamsRepository{db:db,bucket:"photos",prepared:prepared}
 scope:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"task_params"}
 ticket,err:=repo.Reserve(context.Background(),scope,"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",2)
 if err==nil||ticket.ArtifactID!=uuid.Nil||db.begins!=0{t.Fatal("unqualified reservation reached SQL",err)}
 ref,err=repo.Complete(context.Background(),scope,artifactjson.Reservation{ArtifactID:uuid.New(),Token:uuid.New()},objectstore.Object{Key:"foreign",VersionID:"v1",ContentType:"application/json",Bytes:2})
 if err==nil||ref.ID!=uuid.Nil||db.begins!=0{t.Fatal("unqualified completion reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.PublishManualPerspectiveReplacementParams(ctx,db,objects,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
}

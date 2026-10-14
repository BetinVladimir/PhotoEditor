package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Refusal tests only; a complete qualified positive replacement fixture must
// cover publication/recovery, stale quota and fencing in the deferred phase.
func TestReplacementTransportRejectsBeforeSQLAndIO(t *testing.T){
 db:=&negativeDraftStoreDB{}
 objects:=&negativeParamsObjects{}
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 graph,err:=prepared.PublishNegativeReplacementTaskTransport(context.Background(),db,objects,"photos",nil,nil,nil,nil,NegativeReplacementDraft{raw:"{}"},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{})
 if err==nil||graph.ID!=uuid.Nil||db.begins!=0||objects.puts!=0||objects.recoveries!=0{t.Fatal("incomplete transport reached SQL/IO",err)}
 repository:=negativeReplacementTransportRepository{params:negativeReplacementParamsRepository{db:db,bucket:"photos",prepared:prepared},body:"{}"}
 scope:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"v2_input_manifest"}
 ticket,err:=repository.Reserve(context.Background(),scope,"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",2)
 if err==nil||ticket.ArtifactID!=uuid.Nil||db.begins!=0{t.Fatal("unqualified transport reservation reached SQL",err)}
 ref,err:=repository.Complete(context.Background(),scope,artifactjson.Reservation{ArtifactID:uuid.New(),Token:uuid.New()},objectstore.Object{Key:"foreign",VersionID:"v1",ContentType:"application/json",Bytes:2})
 if err==nil||ref.ID!=uuid.Nil||db.begins!=0{t.Fatal("unqualified completion reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.PublishNegativeReplacementTaskTransport(ctx,db,objects,"photos",nil,nil,nil,nil,NegativeReplacementDraft{},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
}

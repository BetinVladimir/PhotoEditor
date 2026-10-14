package v2optical
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestManualPerspectiveReplacementTransportReadRefusesIncompletePackageBeforeSQL(t *testing.T){
 db:=&negativeDraftStoreDB{};prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 graph,err:=prepared.ReadPublishedManualPerspectiveReplacementTransport(context.Background(),db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{raw:"{}"},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{})
 if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("incomplete recovery reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.ReadPublishedManualPerspectiveReplacementTransport(ctx,db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
}

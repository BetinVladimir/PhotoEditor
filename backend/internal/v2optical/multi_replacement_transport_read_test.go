package v2optical
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestMultiReplacementTransportReadRefusesIncompletePackageBeforeSQL(t *testing.T){
 db:=&negativeDraftStoreDB{};prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 graph,err:=prepared.ReadPublishedMultiReplacementTransport(context.Background(),db,"photos",nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{})
 if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("incomplete recovery reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.ReadPublishedMultiReplacementTransport(ctx,db,"photos",nil,nil,nil,nil,MultiReplacementDraft{},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
}

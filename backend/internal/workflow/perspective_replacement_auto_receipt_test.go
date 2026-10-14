package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveReceiptRejectsMissingAndCancelledAuthority(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.ReadAutomaticPerspectiveReceipt(context.Background(),ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing authority returned receipt")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.ReadAutomaticPerspectiveReceipt(ctx,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}

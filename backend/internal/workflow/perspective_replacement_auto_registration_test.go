package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveRegistrationRejectsMissingAuthority(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.RegisterAutomaticPerspectiveInputs(context.Background(),v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("unproven inputs registered")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.RegisterAutomaticPerspectiveInputs(ctx,v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}

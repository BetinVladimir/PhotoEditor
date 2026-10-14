package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveAdmissionRequiresCurrentAuthority(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.AdmitAutomaticPerspectiveGraph(context.Background(),v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing authority admitted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.AdmitAutomaticPerspectiveGraph(ctx,v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}

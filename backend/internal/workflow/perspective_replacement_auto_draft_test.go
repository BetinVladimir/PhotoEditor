package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveDraftRejectsUnprovenAuthority(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.LockAutomaticPerspectiveDraft(context.Background(),nil,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("unproven draft accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.LockAutomaticPerspectiveDraft(ctx,nil,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}
func TestAutomaticPerspectiveDraftSaveRequiresCurrentSources(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.SaveAutomaticPerspectiveDraft(context.Background(),ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing sources saved draft")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.SaveAutomaticPerspectiveDraft(ctx,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}
func TestAutomaticPerspectiveDraftLoadRequiresCurrentSources(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.LoadAutomaticPerspectiveDraft(context.Background(),ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing sources recovered draft")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.LoadAutomaticPerspectiveDraft(ctx,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}

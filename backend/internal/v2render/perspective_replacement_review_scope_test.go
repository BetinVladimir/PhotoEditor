package v2render

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementReviewScopeStrictIdentity(t *testing.T) {
 v:=PerspectiveReplacementReviewScope{Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),OriginalRun:uuid.New(),OriginalTask:uuid.New(),DecisionHash:strings.Repeat("d",64),Base:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}}
 namespace:=uuid.NewSHA1(v.Tenant,[]byte("V29.NegativeReplacement/v2:"+v.Project.String()+":"+v.Review.String()+":"+v.DecisionHash))
 v.Run=uuid.NewSHA1(namespace,[]byte("run:"+v.OriginalRun.String()+":"+v.OriginalTask.String()));v.Workflow,_=v2input.WorkflowID(v.Run);v.Task=uuid.NewSHA1(namespace,[]byte("task:"+v.OriginalTask.String()))
 raw,err:=canonical.Marshal(v);if err!=nil { t.Fatal(err) }
 got,err:=decodePerspectiveReplacementReviewScope(raw,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash);if err!=nil||got!=v { t.Fatal(got,err) }
 for _,alter:=range []func(*PerspectiveReplacementReviewScope){
  func(p *PerspectiveReplacementReviewScope){p.Project=uuid.New()},
  func(p *PerspectiveReplacementReviewScope){p.Review=uuid.New()},
  func(p *PerspectiveReplacementReviewScope){p.OriginalRun=uuid.New()},
  func(p *PerspectiveReplacementReviewScope){p.Task=uuid.New()},
  func(p *PerspectiveReplacementReviewScope){p.Workflow=uuid.New()},
  func(p *PerspectiveReplacementReviewScope){p.Base.Source.SHA256="bad"},
 } {
  bad:=v;alter(&bad);body,_:=canonical.Marshal(bad)
  if _,err=decodePerspectiveReplacementReviewScope(body,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash);err==nil { t.Fatal("substituted identity accepted") }
 }
 extra:=append([]byte(`{"unknown":true,`),raw[1:]...)
 if _,err=decodePerspectiveReplacementReviewScope(extra,v.Tenant,v.Project,v.Review,v.OriginalTask,v.DecisionHash);err==nil { t.Fatal("unknown field accepted") }
}
func TestReplacementReviewScopeUnavailableAndCancellation(t *testing.T) {
 service:=PerspectiveProposalService{}
 if _,err:=service.ReplacementReviewScope(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),uuid.New(),strings.Repeat("a",64));err==nil { t.Fatal("missing authority accepted") }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.ReplacementReviewScope(ctx,auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,uuid.Nil,"");err!=context.Canceled { t.Fatal(err) }
}

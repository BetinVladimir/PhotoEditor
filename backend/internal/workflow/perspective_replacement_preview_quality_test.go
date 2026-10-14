package workflow

import (
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2render"
)

func TestReplacementPerspectivePreviewAndQualityPreserveIdentity(t *testing.T) {
 bundle := perspectivePlanBundleWithSource(t, func(p *v2render.PerspectivePreviewSource) {
  id := v2render.PerspectivePreviewReplacementIdentity{Review:uuid.New(), OriginalTask:uuid.New(), OriginalRun:uuid.New(), DecisionHash:strings.Repeat("d",64)}
  namespace := uuid.NewSHA1(p.Tenant, []byte("V29.NegativeReplacement/v2:"+p.Project.String()+":"+id.Review.String()+":"+id.DecisionHash))
  p.Replacement=id
  p.Run=uuid.NewSHA1(namespace, []byte("run:"+id.OriginalRun.String()+":"+id.OriginalTask.String()))
  p.Producer=uuid.NewSHA1(namespace, []byte("task:"+id.OriginalTask.String()))
  var err error
  p.Workflow,err=v2input.WorkflowID(p.Run)
  if err!=nil { t.Fatal(err) }
 })
 preview,err:=CompilePerspectivePreviewPlan(bundle,strings.Repeat("e",64))
 if err!=nil { t.Fatal(err) }
 quality,err:=CompilePerspectiveQualityPlan(bundle)
 if err!=nil { t.Fatal(err) }
 if preview.Source()!=bundle.Source() || quality.Source()!=bundle.Source() { t.Fatal("replacement provenance lost") }
 if err=validatePerspectivePreviewIntent(preview);err!=nil { t.Fatal(err) }
 if err=validatePerspectiveQualityIntent(quality);err!=nil { t.Fatal(err) }
 for _,field:=range []string{"tenant","project","review","decision","original_run","original_task","missing","ordinary_producer"} {
  t.Run(field,func(t *testing.T) {
   p:=bundle.Source()
   switch field {
   case "tenant":p.Tenant=uuid.New()
   case "project":p.Project=uuid.New()
   case "review":p.Replacement.Review=uuid.New()
   case "decision":p.Replacement.DecisionHash=strings.Repeat("f",64)
   case "original_run":p.Replacement.OriginalRun=uuid.New()
   case "original_task":p.Replacement.OriginalTask=uuid.New()
   case "missing":p.Replacement=v2render.PerspectivePreviewReplacementIdentity{}
   case "ordinary_producer":p.Producer,_=v2input.TaskID(p.Workflow,"optical.perspective",0)
   }
   badPreview:=preview;badPreview.source=p
   badQuality:=quality;badQuality.source=p
   if validatePerspectivePreviewIntent(badPreview)==nil { t.Fatal("substituted preview identity accepted") }
   if validatePerspectiveQualityIntent(badQuality)==nil { t.Fatal("substituted quality identity accepted") }
   params:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(badQuality.Params())}
   if _,err:=PreparePerspectiveQualityTask(badQuality,params);err==nil { t.Fatal("direct task preparation accepted substituted identity") }
  })
 }
}

func TestPerspectiveQualityTaskRejectsDirectSourceSubstitution(t *testing.T) {
 plan,err:=CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
 if err!=nil { t.Fatal(err) }
 params:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(plan.Params())}
 if _,err=PreparePerspectiveQualityTask(plan,params);err!=nil { t.Fatal(err) }
 for _,alter:=range []func(*v2render.PerspectivePreviewSource){
  func(p *v2render.PerspectivePreviewSource){p.Workflow=uuid.New()},
  func(p *v2render.PerspectivePreviewSource){p.Base.AssetVersionID=uuid.Nil},
  func(p *v2render.PerspectivePreviewSource){p.Base.CanvasHash="invalid"},
  func(p *v2render.PerspectivePreviewSource){p.Principal.UserID=uuid.Nil},
  func(p *v2render.PerspectivePreviewSource){p.Attempt=0},
  func(p *v2render.PerspectivePreviewSource){p.Fence=0},
 } {
  bad:=plan;alter(&bad.source)
  if _,err=PreparePerspectiveQualityTask(bad,params);err==nil { t.Fatal("direct preparation bypassed source identity") }
 }
}

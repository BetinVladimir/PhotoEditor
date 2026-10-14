package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type statsPersonOriginSourceStub struct{calls int}
func(s *statsPersonOriginSourceStub)LookupExportStatsPersonOrigin(context.Context,v2input.Entry,v2input.Ref,[]byte,review.WorkingImage)(ExportStatsPersonOrigin,error){s.calls++;return ExportStatsPersonOrigin{},nil}
type statsPersonPolicyDenied struct{calls int}
func(s *statsPersonPolicyDenied)ResolveExportStatsPersonPolicy(context.Context,ExportStatsPersonOrigin,v2input.Ref)(ExportStatsPersonPolicyEvidence,error){s.calls++;return ExportStatsPersonPolicyEvidence{},v2input.ErrInvalid}
func TestQualifiedStatsPersonSourceRequiresCurrentPolicyBeforeIO(t *testing.T){
 for _,mode:=range []string{"denied","missing-policy","missing-origin","hash","cancelled"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  raw,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:imageEntry.Ref.ArtifactID,SHA256:imageEntry.Ref.SHA256},Person:review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}});if err!=nil{t.Fatal(err)}
  origin:=&statsPersonOriginSourceStub{};policy:=&statsPersonPolicyDenied{}
  source:=QualifiedExportStatsPersonSource{Origin:origin,Policy:policy,Documents:ExportStatsPersonDocumentReader{Catalog:&statsPersonDocumentCatalogStub{},Objects:exportDefectNoIO{t}}}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)};ctx:=context.Background()
  switch mode{case "missing-policy":source.Policy=nil;case "missing-origin":source.Origin=nil;case "hash":ref.SHA256=rawSHA([]byte("different"));case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  result,err:=source.LookupExportStatsPerson(ctx,v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("stats"))},Contract:"Stats20"},ref,raw,image)
  if err==nil||result!=(objectstore.Object{}){t.Fatal("unqualified source admitted",err)}
  expected:=0;if mode=="denied"{expected=1};if origin.calls!=expected||policy.calls!=expected{t.Fatal("authority ordering",origin.calls,policy.calls)}
 })}
}

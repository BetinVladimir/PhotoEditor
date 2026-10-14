package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderParentReader interface {
 ReadDelegatedRenderParents(context.Context,CanonicalSigningTask)([]SigningRenderParent,error)
}

type BoundDelegatedRenderEvidenceSource struct {
 Adopted DelegatedAdoptedResultPort
 Documents DelegatedTaskDocuments
 Counter DelegatedRenderMaskCounter
 Pixels BoundDelegatedRenderPixelsVerifier
 Qualification DelegatedRenderQualificationReader
 Parents DelegatedRenderParentReader
 Reports SigningImageReportReader
}

func(s BoundDelegatedRenderEvidenceSource)ReadDelegatedRenderEvidence(ctx context.Context,
 task CanonicalSigningTask)(SigningRenderEvidence,error){
 var zero SigningRenderEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Adopted==nil||s.Documents==nil||s.Counter==nil||s.Pixels.Catalog==nil||
  s.Pixels.Objects==nil||s.Qualification==nil||s.Parents==nil||s.Reports==nil{
  return zero,ErrInvalid
 }
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 if owned.Operation!="render.compose"||owned.Version!="1"{return zero,ErrInvalid}
 pixels,err:=ReadDelegatedRenderPixelEvidence(ctx,s.Adopted,s.Documents,s.Counter,owned)
 if err!=nil{return zero,err}
 if err:=s.Pixels.Verify(ctx,owned,pixels);err!=nil{return zero,err}
 recipeBytes,err:=canonical.Marshal(pixels.recipe)
 if err!=nil{return zero,err}
 dependencies,err:=v2draft.FrozenRenderRecipeDependencies(recipeBytes,owned.Params)
 if err!=nil{return zero,ErrInvalid}
 qualification,err:=s.Qualification.ReadDelegatedRenderQualification(ctx,owned,pixels.recipe)
 if err!=nil{return zero,err}
 if !qualification.valid()||qualification.ModelLockSHA!=pixels.recipe.Scalars.ModelLockSHA{
  return zero,ErrInvalid
 }
 parents,err:=s.Parents.ReadDelegatedRenderParents(ctx,owned)
 if err!=nil{return zero,err}
 if parents==nil||len(parents)>33{return zero,ErrInvalid}
 parents,frozenParents,err:=ownSigningRenderParents(parents)
 if err!=nil{return zero,err}
 roots:=[]v2input.Ref{}
 bodies:=map[v2input.Ref][]byte{}
 for _,parent:=range parents{
  declared,err:=canonicalTaskOutputRefs(parent.Producer.Outputs)
  if err!=nil||len(declared)!=1||declared[0]!=parent.Root{return zero,ErrInvalid}
  raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,parent.Producer,parent.Root,1<<20)
  if err!=nil{return zero,err}
  if !pinned(raw,parent.Root,1<<20){return zero,ErrInvalid}
  body:=append([]byte(nil),raw...)
  result,err:=v2draft.ParseImageResult(body,parent.Root)
  if err!=nil{return zero,ErrInvalid}
  bodies[parent.Root]=body
  roots=append(roots,result.Report)
 }
 inherited,err:=CollectSigningRenderParentReports(ctx,s.Reports,roots,
  pixels.recipe.Scalars.Source)
 if err!=nil{return zero,err}
 report,err:=ReadDelegatedRenderReport(ctx,s.Documents,owned,
  pixels.recipe.Scalars.Source,pixels.result.Report,inherited.Reports)
 if err!=nil{return zero,err}
 if inherited.ReviewRequired&&report.Status!="review"{return zero,ErrInvalid}
 for _,parent:=range parents{
  raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,parent.Producer,parent.Root,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(raw,bodies[parent.Root]){return zero,ErrInvalid}
 }
 current,err:=s.Parents.ReadDelegatedRenderParents(ctx,owned)
 if err!=nil{return zero,err}
 currentRaw,err:=canonical.Marshal(current)
 if err!=nil||!bytes.Equal(currentRaw,frozenParents){return zero,ErrInvalid}
 currentRoot,_,err:=s.Adopted.ReadDelegatedAdoptedResult(ctx,owned)
 if err!=nil||currentRoot!=pixels.root{return zero,ErrInvalid}
 currentQualification,err:=s.Qualification.ReadDelegatedRenderQualification(ctx,owned,pixels.recipe)
 if err!=nil||currentQualification!=qualification{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 taskDigest,err:=canonical.Hash(owned)
 if err!=nil{return zero,err}
 return SigningRenderEvidence{taskDigest:taskDigest,pixels:pixels,report:report,
  parents:parents,inheritedReview:inherited.ReviewRequired,
  qualification:qualification,dependencies:dependencies},nil
}

var _ DelegatedRenderEvidenceReader=BoundDelegatedRenderEvidenceSource{}

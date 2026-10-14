package v2learning

import (
 "bytes"
 "context"
 "encoding/json"
 "math"

 "github.com/google/uuid"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// TrainingRegionSource uses only the event's registered immutable closure.
// It never synthesizes masks or uses an unscoped selector/model lookup.
type TrainingRegionSource struct {
 Catalog v2draft.ArtifactCatalog
 Objects LearningEvaluateObjectReader
 Selector TrainingMaskSelector
}
func (s TrainingRegionSource) LoadRegions(ctx context.Context, who auth.Identity,
 start TrainingStart, candidate TrainingCandidate, image TrainingImage,
 style v2input.Ref) ([]TrainingRegionMask,error) {
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Catalog==nil||s.Objects==nil||!validManifestRef(style)||start.TenantID==uuid.Nil||start.ProjectID==uuid.Nil||candidate.EventID==uuid.Nil||candidate.AssetVersionID==uuid.Nil||image.PixelsCount<1||image.Document.Canvas.SourceSHA256!=candidate.SourceLineageSHA{return nil,ErrInvalidTrainingDraft}
 root,err:=s.Catalog.Lookup(ctx,who,start.TenantID,v2draft.LayerRef{Path:"TrainingRenderCase.base_style",Contract:"StyleV2",Ref:style})
 if err!=nil{return nil,err}
 body,err:=ReadFrozenRootDocument(ctx,s.Objects,root,style)
 if err!=nil{return nil,err}
 return s.Selector.SelectRegions(ctx,who,start,candidate,image,body)
}

func (s TrainingMaskSelector) SelectRegions(ctx context.Context, who auth.Identity,
 start TrainingStart, candidate TrainingCandidate, image TrainingImage,
 styleRaw []byte) ([]TrainingRegionMask,error) {
 if err:=ctx.Err();err!=nil{return nil,err}
 var style struct { Extensions struct { Regions []struct {
  ID string `json:"region_id"`
  Selector string `json:"selector"`
 } `json:"region_adjustments"` } `json:"extensions"` }
 if len(styleRaw)<2||len(styleRaw)>8<<20||json.Unmarshal(styleRaw,&style)!=nil||len(style.Extensions.Regions)>128{return nil,ErrInvalidTrainingDraft}
 rules:=style.Extensions.Regions
 if len(rules)==0{return nil,nil}
 if s.Roots==nil||s.Reader==nil||s.Masks==nil||image.PixelsCount<1{return nil,ErrInvalidTrainingDraft}
 ids:=map[string]bool{}
 selectors:=map[string]bool{}
 for _,rule:=range rules {
  if len(rule.ID)<1||len(rule.ID)>120||ids[rule.ID]||len(rule.Selector)<1||len(rule.Selector)>120{return nil,ErrInvalidTrainingDraft}
  ids[rule.ID]=true;selectors[rule.Selector]=true
 }
 refs,err:=s.Roots.List(ctx,start,ownTrainingAssemblyCandidate(candidate))
 if err!=nil{return nil,err}
 if len(refs)>1024{return nil,ErrInvalidTrainingDraft}
 // Own callback output before any subsequent reader can mutate its backing array.
 refs=append([]v2input.Ref(nil),refs...)
 chosen:=map[string]v2input.Ref{}
 masks:=map[string]review.MaskRef{}
 seen:=map[v2input.Ref]bool{}
 for _,ref:=range refs {
  if err:=ctx.Err();err!=nil{return nil,err}
  if !validManifestRef(ref)||seen[ref]{return nil,ErrInvalidTrainingDraft};seen[ref]=true
  body,err:=s.Reader.Read(ctx,who,start.TenantID,ref)
  if err!=nil{return nil,err}
  if err:=ctx.Err();err!=nil{return nil,err}
  parsed,err:=review.ParseMaskRef(body)
  if err!=nil{continue}
  if parsed.Protected||!selectors[parsed.Selector]||math.IsNaN(parsed.Confidence)||math.IsInf(parsed.Confidence,0)||parsed.Confidence<.75||parsed.Confidence>1||parsed.Canvas!=image.Document.Canvas||!bytes.Equal(parsed.TransformChain,image.Document.TransformChain){continue}
  if _,exists:=chosen[parsed.Selector];exists{return nil,ErrInvalidTrainingExample}
  chosen[parsed.Selector]=ref;masks[parsed.Selector]=parsed
 }
 result:=make([]TrainingRegionMask,0,len(rules))
 validated:=map[string]TrainingMask{}
 for _,rule:=range rules {
  ref,ok:=chosen[rule.Selector];if !ok{return nil,ErrInvalidTrainingExample}
  if _,ok:=validated[rule.Selector];!ok {
   expected,err:=canonical.Marshal(masks[rule.Selector]);if err!=nil{return nil,ErrInvalidTrainingDraft}
   mask,err:=s.Masks.Load(ctx,who,start,ownTrainingAssemblyCandidate(candidate),ref,image,false)
   if err!=nil{return nil,err}
   if err:=ctx.Err();err!=nil{return nil,err}
   actual,err:=canonical.Marshal(mask.Document)
   if err!=nil||!bytes.Equal(expected,actual)||mask.Pixels!=(v2input.Ref{ArtifactID:masks[rule.Selector].Artifact.ID,SHA256:masks[rule.Selector].Artifact.SHA256}){return nil,ErrInvalidTrainingDraft}
   validated[rule.Selector]=mask
  }
  result=append(result,TrainingRegionMask{RegionID:rule.ID,Mask:ref})
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return result,nil
}

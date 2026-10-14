package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestImageLocalScopeRejectsMisalignedBaseBeforePolicy(t *testing.T){
 for _,mode:=range []string{"valid","canvas","color","alpha","hash","mask","style_skip","style_hash","unused_region","duplicate_rule","item_without_series","series_valid","series_missing_item","series_duplicate_item"}{t.Run(mode,func(t *testing.T){
  _,source,sourceRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{t.Fatal(err)}
  base:=image
  switch mode{case "canvas":base.Canvas.Width++;case "color":base.Color.ICCSHA256=rawSHA([]byte("other icc"));case "alpha":base.Alpha=&review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("alpha"))}}
  baseRaw,err:=canonical.Marshal(base);if err!=nil{t.Fatal(err)}
  baseEntry:=v2input.Entry{Contract:"WorkingImage",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(baseRaw)}}
  reader:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{source.Ref.ArtifactID:{Entry:source,Body:sourceRaw},baseEntry.Ref.ArtifactID:{Entry:baseEntry,Body:baseRaw}}}
  if mode=="hash"{node:=reader.nodes[baseEntry.Ref.ArtifactID];node.Body=[]byte("{}");reader.nodes[baseEntry.Ref.ArtifactID]=node}
  style:=map[string]any{"mode":"apply","params":map[string]any{"image":source.Ref},"modules":map[string]any{"extensions":map[string]any{"region_adjustments":[]any{}}}}
  if mode=="style_skip"{style["mode"]="skip"}
  if mode=="duplicate_rule"{style["modules"]=map[string]any{"extensions":map[string]any{"region_adjustments":[]any{map[string]any{"region_id":"r1"},map[string]any{"region_id":"r1"}}}}}
  styleRaw,err:=canonical.Marshal(style);if err!=nil{t.Fatal(err)}
  styleEntry:=v2input.Entry{Contract:"ResolvedStyle",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(styleRaw)}}
  styleNode:=ExportClosureNode{Entry:styleEntry,Body:styleRaw,Children:[]v2input.Entry{source}}
  if mode=="style_hash"{styleNode.Body=[]byte("{}")}
  // Mock reader supplies already schema/producer-qualified style; these tests
  // exercise only the additional cross-document constraints.
  styles:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{styleEntry.Ref.ArtifactID:styleNode}}
  params:=map[string]any{"image":source.Ref,"base_aligned":baseEntry.Ref,"resolved":styleEntry.Ref}
  if mode=="mask"{mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1};mask.Canvas.Width++;params["person"]=mask}
  if mode=="unused_region"{params["regions"]=[]any{map[string]any{"region_id":"unused","mask":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}}}}
  if mode=="item_without_series"{params["item_id"]=uuid.New()}
  if mode=="series_valid"||mode=="series_missing_item"||mode=="series_duplicate_item"{
   selected:=uuid.New();ids:=[]uuid.UUID{selected,uuid.New()}
   if mode=="series_missing_item"{ids[0]=uuid.New()};if mode=="series_duplicate_item"{ids[1]=selected}
   seriesRaw,err:=canonical.Marshal(map[string]any{"items":[]any{map[string]any{"item_id":ids[0]},map[string]any{"item_id":ids[1]}}});if err!=nil{t.Fatal(err)}
   seriesEntry:=v2input.Entry{Contract:"SeriesCorrections",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(seriesRaw)}}
   styles.nodes[seriesEntry.Ref.ArtifactID]=ExportClosureNode{Entry:seriesEntry,Body:seriesRaw}
   params["series"]=seriesEntry.Ref;params["item_id"]=selected
  }
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  other:=&resolvedOriginOtherSpy{}
  scope:=ExportImageLocalSourceScope{Images:reader,Resolved:styles,Other:other}
  _,err=scope.ExportDocumentChildren(context.Background(),v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}},raw)
  if mode=="valid"||mode=="series_valid"{if err!=nil||other.calls!=1{t.Fatal(err,other.calls)}}else if err==nil||other.calls!=0{t.Fatal("misaligned input reached policy",mode,err,other.calls)}
 })}
}

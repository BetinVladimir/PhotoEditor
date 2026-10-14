package interchange
import (
 "testing"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestExportInlineStatsMaskExtractionIsClosedAndOwned(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 params:=review.StatsParams{Image:review.ArtifactRef{ID:source.Ref.ArtifactID,SHA256:source.Ref.SHA256},Person:review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}}
 for _,mode:=range []string{"valid","selector","hash","contract"}{t.Run(mode,func(t *testing.T){
  owned:=params;if mode=="selector"{owned.Person.Selector="manual"}
  raw,err:=canonical.Marshal(owned);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Params"}
  if mode=="hash"{entry.Ref.SHA256=rawSHA([]byte("foreign"))};if mode=="contract"{entry.Contract="Override"}
  masks,err:=exportParamsInlineMasks(entry,raw)
  if mode!="valid"{if err==nil{t.Fatal("invalid Params accepted")};return}
  if err!=nil||len(masks)!=1{t.Fatal("inline person mask missing",err)}
  original:=rawSHA(raw);masks[0][0]='!';if rawSHA(raw)!=original{t.Fatal("extracted mask aliases Params")}
 })}
}

type paramsMaskAuthorityStub struct{calls int;deny bool}
func(s *paramsMaskAuthorityStub)QualifyExportParamsMask(context.Context,v2input.Entry,[]byte,v2input.Ref)error{s.calls++;if s.deny{return v2input.ErrInvalid};return nil}
func TestParamsInlineMaskGeometryRequiresCurrentProducer(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 params:=review.StatsParams{Image:review.ArtifactRef{ID:source.Ref.ArtifactID,SHA256:source.Ref.SHA256},Person:review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}}
 raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Params"};ref:=v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}
 reader:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{parent.Ref.ArtifactID:{Entry:parent,Body:raw,Children:[]v2input.Entry{{Ref:ref,Contract:"MaskPixels"},source}},source.Ref.ArtifactID:{Entry:source,Body:imageRaw}}}
 authority:=&paramsMaskAuthorityStub{}
 geometry:=ExportAllMaskGeometrySource{Reader:reader,Parents:[]v2input.Entry{parent},ParamsAuthority:authority}
 if _,err:=geometry.ResolveExportMaskGeometry(context.Background(),ref);err!=nil||authority.calls!=1{t.Fatal("Params geometry",err)}
 sourceNode:=reader.nodes[source.Ref.ArtifactID];changed:=sourceNode;changed.Body=[]byte("{}");reader.nodes[source.Ref.ArtifactID]=changed
 if _,err:=geometry.ResolveExportMaskGeometry(context.Background(),ref);err==nil{t.Fatal("changed actual source accepted")}
 reader.nodes[source.Ref.ArtifactID]=sourceNode
 authority.deny=true;if _,err:=geometry.ResolveExportMaskGeometry(context.Background(),ref);err==nil{t.Fatal("producer refusal bypassed")}
 geometry.ParamsAuthority=nil;if _,err:=geometry.ResolveExportMaskGeometry(context.Background(),ref);err==nil{t.Fatal("missing producer accepted")}
}

func TestParamsMaskSourceRejectsForeignCanvasBeforeWarpAuthority(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1};mask.Canvas.SourceSHA256=rawSHA([]byte("foreign encoded source"))
 maskRaw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
 raw,err:=canonical.Marshal(map[string]any{"image":source.Ref});if err!=nil{t.Fatal(err)}
 reader:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{source.Ref.ArtifactID:{Entry:source,Body:imageRaw}}}
 parent:=ExportClosureNode{Body:raw,Children:[]v2input.Entry{source}}
 if err:=qualifyExportParamsMaskSource(context.Background(),reader,nil,parent,[]json.RawMessage{maskRaw});err==nil{t.Fatal("foreign mask canvas accepted")}
}

func TestPortraitDetailInlineMasksUseClosedTypedParams(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","confirmation","strength","region-id","protected","defaults","null-strength","null-protected"}{t.Run(mode,func(t *testing.T){
  target:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}
  guard:=target;guard.Artifact.ID=uuid.New();guard.Protected=true
  region:=map[string]any{"region_id":"cloth-1","kind":"cloth","mask":target,"confirmed":true,"strength":.5}
  switch mode{case "confirmation":region["confirmed"]=false;case "strength":region["strength"]=2;case "region-id":region["region_id"]="invalid id";case "protected":guard.Protected=false;case "defaults":delete(region,"strength");case "null-strength":region["strength"]=nil}
  value:=map[string]any{"image":source.Ref,"resolved":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("resolved"))},"regions":[]any{region},"protected":[]any{guard}}
  if mode=="defaults"{delete(value,"protected")};if mode=="null-protected"{value["protected"]=nil}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Params"}
  masks,err:=exportParamsInlineMasks(entry,raw)
  if mode=="valid"||mode=="defaults"{expected:=2;if mode=="defaults"{expected=1};if err!=nil||len(masks)!=expected{t.Fatal("portrait masks missing",err)}}else if err==nil{t.Fatal("invalid portrait Params admitted",mode)}
 })}
}

func TestImageLocalInlineMasksAndTypedDependencies(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
 for _,mode:=range []string{"valid","defaults","null_regions","duplicate_regions","unknown_field","bad_selector"}{t.Run(mode,func(t *testing.T){
  params:=map[string]any{"image":source.Ref,"base_aligned":source.Ref,"resolved":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("resolved"))},"person":mask,"regions":[]any{map[string]any{"region_id":"r1","mask":mask}}}
  switch mode{case "defaults":delete(params,"person");delete(params,"regions");case "null_regions":params["regions"]=nil;case "duplicate_regions":params["regions"]=[]any{map[string]any{"region_id":"r1","mask":mask},map[string]any{"region_id":"r1","mask":mask}};case "unknown_field":params["approved"]=true;case "bad_selector":bad:=mask;bad.Selector="manual";params["person"]=bad}
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  masks,err:=exportParamsInlineMasks(entry,raw)
  if mode!="valid"&&mode!="defaults"{if err==nil{t.Fatal("invalid local Params accepted")};return}
  if err!=nil{t.Fatal(err)};expected:=2;if mode=="defaults"{expected=0};if len(masks)!=expected{t.Fatal("mask inventory",len(masks))}
 })}
}

func TestExportBodyParamsAreNotSelectedAsStatsInlineMasks(t *testing.T){
 for _,poses:=range []any{nil,v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("poses"))}}{ 
  raw,err:=canonical.Marshal(map[string]any{"image":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))},"person":nil,"poses":poses});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  operation,err:=exportParamsOperationShape(raw);if err!=nil||operation!="geometry.body"{t.Fatal("body selected as stats",operation,err)}
  masks,err:=exportParamsInlineMasks(entry,raw);if err==nil||len(masks)!=0{t.Fatal("malformed body mask accepted",err)}
 }
}

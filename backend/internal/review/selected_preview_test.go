package review

import (
 "context"
 "math"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/media"
 "photoeditor/backend/internal/objectstore"
)

type renderSelectionReaderFunc func(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(RenderResultSelection,error)
func(f renderSelectionReaderFunc)ReadRenderResultSelection(ctx context.Context,who auth.Identity,tid,id uuid.UUID)(RenderResultSelection,error){return f(ctx,who,tid,id)}

func TestSelectedRenderPreviewNeverPublishesSkippedImage(t *testing.T){
 tenant,reviewID:=uuid.New(),uuid.New()
 reader:=renderSelectionReaderFunc(func(_ context.Context,_ auth.Identity,tid,id uuid.UUID)(RenderResultSelection,error){
  if tid!=tenant||id!=reviewID{t.Fatal("selection scope changed")}
  return RenderResultSelection{SchemaVersion:1,ReviewID:id,DecisionHash:strings.Repeat("a",64),Route:"skip"},nil
 })
 service:=CandidatePreviewService{Selection:reader}
 _,err:=service.SelectedRenderPreview(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,reviewID,"preview1600")
 if domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("skipped render received a preview",err)}
}

func TestIssuedSelectedPreviewRechecksDecisionAfterCreatingLinks(t *testing.T) {
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 tenant,reviewID:=uuid.New(),uuid.New()
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 sha:=strings.Repeat("a",64)
 source:=ArtifactRef{ID:uuid.New(),SHA256:sha}
 manifest:=ArtifactRef{ID:uuid.New(),SHA256:sha}
 derivative:=ArtifactRef{ID:uuid.New(),SHA256:sha}
 objects:=CandidatePreviewObjects{Source:source,Manifest:manifest,Derivative:derivative,ProfileHash:sha,
  ManifestObject:objectstore.Object{Key:"manifest",VersionID:"v1",SHA256:sha,ContentType:"application/json",Bytes:100},
  DerivativeObject:objectstore.Object{Key:"image",VersionID:"v2",SHA256:sha,ContentType:"image/jpeg",Bytes:100}}
 preview:=CandidatePreview{Review:reviewID,Side:"before",Source:source,ProfileHash:sha,Width:100,Height:100,
  Manifest:CandidatePreviewLink{Artifact:manifest,Bytes:100,MediaType:"application/json",URL:"https://media.test/manifest?versionId=v1",ExpiresAt:now.Add(5*time.Minute)},
  Image:CandidatePreviewLink{Artifact:derivative,Bytes:100,MediaType:"image/jpeg",URL:"https://media.test/image?versionId=v2",ExpiresAt:now.Add(5*time.Minute)}}
 selection:=RenderResultSelection{SchemaVersion:1,ReviewID:reviewID,DecisionHash:sha,Route:"base",Image:&source}
 for _,changed:=range []bool{false,true}{
  t.Run(map[bool]string{false:"stable",true:"changed"}[changed],func(t *testing.T){
   catalogReads:=0
   service:=CandidatePreviewService{Clock:func()time.Time{return now},
    Catalog:previewCatalogFunc(func(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string)(CandidatePreviewObjects,error){catalogReads++;return objects,nil}),
    Selection:renderSelectionReaderFunc(func(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(RenderResultSelection,error){
     current:=selection
     if changed{current.DecisionHash=strings.Repeat("b",64)}
     return current,nil
    })}
   issued,err:=service.issueSelectedPreview(context.Background(),who,tenant,reviewID,"preview1600",
    []byte(strings.Repeat("k",32)),"https://api.test","render",preview,selection)
   if changed{
    if domain.AsError(err).Code!="STALE_CANDIDATE_REVIEW"||issued.Review!=uuid.Nil{t.Fatal("stale decision issued links",err)}
   }else if err!=nil||issued.Review!=reviewID||catalogReads!=2{
    t.Fatal("stable selected preview unavailable",err,catalogReads)
   }
  })
 }
}

func TestSelectedRenderPreviewRejectsUnboundSelectionBeforePhysicalLookup(t *testing.T) {
 tenant,reviewID:=uuid.New(),uuid.New()
 image:=ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}
 valid:=RenderResultSelection{SchemaVersion:1,ReviewID:reviewID,DecisionHash:strings.Repeat("b",64),Route:"base",Image:&image}
 for _,tc:=range []struct{name string;mutate func(*RenderResultSelection)}{
  {"wrong_review",func(v *RenderResultSelection){v.ReviewID=uuid.New()}},
  {"wrong_schema",func(v *RenderResultSelection){v.SchemaVersion=2}},
  {"bad_decision_hash",func(v *RenderResultSelection){v.DecisionHash="bad"}},
  {"missing_image",func(v *RenderResultSelection){v.Image=nil}},
  {"skip_with_image",func(v *RenderResultSelection){v.Route="skip"}},
 } {
  t.Run(tc.name,func(t *testing.T){
   selection:=valid;tc.mutate(&selection)
   reader:=renderSelectionReaderFunc(func(_ context.Context,_ auth.Identity,tid,id uuid.UUID)(RenderResultSelection,error){
    if tid!=tenant||id!=reviewID{t.Fatal("reader scope changed")}
    return selection,nil
   })
   catalogCalls:=0
   catalog:=previewCatalogFunc(func(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string)(CandidatePreviewObjects,error){catalogCalls++;return CandidatePreviewObjects{},nil})
   service:=CandidatePreviewService{Selection:reader,Catalog:catalog}
   result,err:=service.SelectedRenderPreview(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,reviewID,"preview1600")
   if err==nil||result.Review!=uuid.Nil||catalogCalls!=0{t.Fatal("malformed selection reached physical lookup",err,catalogCalls)}
  })
 }
}

func TestSelectedRenderPreviewPinsRouteSourceAndDecision(t *testing.T) {
 for _,tc:=range []struct{route,side string;change bool}{
  {"base","before",false},{"candidate","after",false},{"candidate","after",true},
 } {
  t.Run(tc.route+map[bool]string{true:"_changed",false:"_valid"}[tc.change],func(t *testing.T){
   raw:=imageFixtures(t)[0]
   parsed,err:=ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
   selected:=ArtifactRef{ID:uuid.New(),SHA256:derivativeManifestSHA(raw)}
   derivative:=ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}
   icc:=strings.Repeat("b",64)
   profile,err:=media.ProfileHash("preview1600",icc);if err!=nil{t.Fatal(err)}
   scale:=math.Min(1,1600/math.Max(float64(parsed.Canvas.Width),float64(parsed.Canvas.Height)))
   mime:="image/jpeg";if parsed.Alpha!=nil{mime="image/webp"}
   manifest:=RenderDerivativeManifest{Source:selected,Derivative:derivative,Variant:"preview1600",ProfileHash:profile,Width:max(1,int(math.RoundToEven(float64(parsed.Canvas.Width)*scale))),Height:max(1,int(math.RoundToEven(float64(parsed.Canvas.Height)*scale))),MediaType:mime,ColorProfile:"srgb",MetadataPolicy:"stripped",Alpha:parsed.Alpha!=nil}
   body,err:=canonical.Marshal(manifest);if err!=nil{t.Fatal(err)}
   manifestSHA:=derivativeManifestSHA(body)
   value:=CandidatePreviewObjects{Source:selected,Manifest:ArtifactRef{ID:uuid.New(),SHA256:manifestSHA},Derivative:derivative,ProfileHash:profile,ManifestObject:objectstore.Object{Key:"manifest",VersionID:"v1",SHA256:manifestSHA,ContentType:"application/json",Bytes:int64(len(body))},DerivativeObject:objectstore.Object{Key:"image",VersionID:"v2",SHA256:derivative.SHA256,ContentType:mime,Bytes:100}}
   tenant,reviewID:=uuid.New(),uuid.New()
   reads,signs:=0,0
   reader:=renderSelectionReaderFunc(func(_ context.Context,_ auth.Identity,tid,id uuid.UUID)(RenderResultSelection,error){
    if tid!=tenant||id!=reviewID{t.Fatal("selection scope changed")}
    reads++
    hash:=strings.Repeat("c",64);if tc.change&&reads==2{hash=strings.Repeat("d",64)}
    return RenderResultSelection{SchemaVersion:1,ReviewID:id,DecisionHash:hash,Route:tc.route,Image:&selected},nil
   })
   catalog:=previewCatalogFunc(func(_ context.Context,_ auth.Identity,tid,id uuid.UUID,side,variant string)(CandidatePreviewObjects,error){
    if tid!=tenant||id!=reviewID||side!=tc.side||variant!="preview1600"{t.Fatal("wrong selected preview lookup",side,variant)}
    return value,nil
   })
   source:=previewSourceFunc(func(_ context.Context,_ auth.Identity,tid,id uuid.UUID,side string)(ArtifactRef,[]byte,error){
    if tid!=tenant||id!=reviewID||side!=tc.side{t.Fatal("wrong selected source lookup",side)}
    return selected,append([]byte(nil),raw...),nil
   })
   now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
   signer:=previewSignerFunc(func(_ context.Context,object objectstore.Object,_ time.Duration)(objectstore.ReadURL,error){
    signs++;return objectstore.ReadURL{URL:"https://media.test/versioned?versionId="+object.VersionID,ExpiresAt:now.Add(5*time.Minute)},nil
   })
   service:=CandidatePreviewService{Selection:reader,Catalog:catalog,Source:source,Objects:serviceObjects{raw:string(body)},Signer:signer,SRGBSHA:icc,Clock:func()time.Time{return now}}
   result,err:=service.SelectedRenderPreview(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},tenant,reviewID,"preview1600")
   if tc.change{
    if domain.AsError(err).Code!="STALE_CANDIDATE_REVIEW"||result.Review!=uuid.Nil{t.Fatal("changed decision yielded preview",err)}
   }else if err!=nil||result.Review!=reviewID||result.Side!=tc.side||result.Source!=selected{t.Fatal("selected preview binding lost",err)}
   if reads!=2||signs!=2{t.Fatal("selection/signing not completed",reads,signs)}
  })
 }
}

package provenance

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type signingNegativeMaskCounterFunc func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.MaskRef)(int64,error)
func(f signingNegativeMaskCounterFunc)CountSigningNegativeMask(c context.Context,a auth.Identity,r ReadySigningExport,t CanonicalSigningTask,p v2input.Ref,m review.MaskRef)(int64,error){return f(c,a,r,t,p,m)}

func TestNegativePixelEvidencePreservesSourceMetadata(t *testing.T){
 raw,err:=os.ReadFile("../v2optical/testdata/negative-source.json");if err!=nil{t.Fatal(err)}
 source,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","alpha","canvas","color","chain","pixel_alias","count","bypass_changed","source_changed"}{
  t.Run(mode,func(t *testing.T){
   pin:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
   sourceRef:=pin();sourceRef.SHA256=bodyDigest(raw)
   canvasSHA,_:=canonical.Hash(source.Canvas)
   params:=v2optical.NegativeParams{SchemaVersion:1,Base:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:sourceRef,CanvasHash:canvasSHA},Strength:1,FilmBaseRGB:[3]float64{.9,.8,.7},Calibration:pin()}
   if mode=="bypass_changed"{params.Strength=0}
   paramsRaw,paramsSHA,err:=params.Compile();if err!=nil{t.Fatal(err)}
   paramsRef:=pin();paramsRef.SHA256=paramsSHA
   image:=source;image.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
   switch mode{
   case "alpha":image.Alpha=&review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("d",64)}
   case "canvas":image.Canvas.Width++
   case "color":image.Color.ICCSHA256=strings.Repeat("f",64)
   case "chain":image.TransformChain=json.RawMessage(`{}`)
   case "pixel_alias":image.Pixels=review.ArtifactRef{ID:sourceRef.ArtifactID,SHA256:sourceRef.SHA256}
   }
   imageRaw,imageRef:=contractBytes(t,image)
   mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("c",64)},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"changed",Confidence:1}
   maskRaw,maskRef:=contractBytes(t,mask)
   resultRaw,resultRef:=contractBytes(t,v2draft.ImageResultDocument{SchemaVersion:1,WorkingImage:imageRef,ChangedMask:maskRef,Report:pin()})
   outputs,_:=canonical.Marshal([]v2input.Ref{resultRef})
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Version:"1",Params:paramsRef,InputDigest:paramsSHA,ResultDigest:paramsSHA,Inputs:json.RawMessage(`[]`),Outputs:outputs,Metrics:json.RawMessage(`{}`)}
   bodies:=map[v2input.Ref][]byte{paramsRef:paramsRaw,sourceRef:raw,imageRef:imageRaw,maskRef:maskRaw,resultRef:resultRaw};sourceReads:=0
   documents:=signingDocumentsFunc(func(_ context.Context,_ auth.Identity,_ ReadySigningExport,_ CanonicalSigningTask,p v2input.Ref,_ int)([]byte,error){
    if p==sourceRef{sourceReads++;if mode=="source_changed"&&sourceReads>1{return []byte(`{}`),nil}}
    body,ok:=bodies[p];if !ok{t.Fatal("unexpected document")};return body,nil
   })
   adoption:=signingNegativeAdoptionFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(v2input.Ref,error){return resultRef,nil})
   counter:=signingNegativeMaskCounterFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.MaskRef)(int64,error){if mode=="count"{return -1,nil};return 1,nil})
   evidence,err:=ReadSigningNegativePixelEvidence(context.Background(),adoption,documents,counter,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New()},task)
   if mode=="valid"{if err!=nil||evidence.root!=resultRef||evidence.changedPixels!=1{t.Fatal("valid evidence refused",err)};return}
   if err==nil{t.Fatal("changed metadata or invalid count accepted",mode)}
  })
 }
}

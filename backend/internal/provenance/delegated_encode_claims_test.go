package provenance

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/workflow"
)

type delegatedDocumentsFunc func(context.Context,CanonicalSigningTask,v2input.Ref,int)([]byte,error)
func(f delegatedDocumentsFunc)ReadDelegatedTaskDocument(ctx context.Context,
 task CanonicalSigningTask,ref v2input.Ref,max int)([]byte,error){return f(ctx,task,ref,max)}
type delegatedImageProducerFunc func(context.Context,v2input.Ref)(uuid.UUID,error)
func(f delegatedImageProducerFunc)ResolveDelegatedImageProducer(ctx context.Context,
 ref v2input.Ref)(uuid.UUID,error){return f(ctx,ref)}

func TestDelegatedEncodeClaimsBindManifestAndProducer(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json")
 if err!=nil{t.Fatal(err)}
 var images []json.RawMessage
 if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing image fixture")}
 imageRaw,err:=canonical.Bytes(images[0])
 if err!=nil{t.Fatal(err)}
 image,err:=review.ParseWorkingImage(imageRaw)
 if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","manifest","changed_parent","mime"}{
  t.Run(mode,func(t *testing.T){
   imageRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:bodyDigest(imageRaw)}
   p:=signingEncodeParams{Image:imageRef,DownloadPreset:workflow.DownloadPreset{
    Format:"png",ICCID:"srgb_v1",Subsampling:"444",Alpha:"preserve",
    MetadataPolicy:"strip"},ICCSHA256:strings.Repeat("a",64)}
   params,paramsRef:=contractBytes(t,p)
   encoded:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
   manifestDoc:=signingEncodedImage{Artifact:encoded,Format:p.Format,
    Width:image.Canvas.Width,Height:image.Canvas.Height,ICCID:p.ICCID,
    ICCSHA:p.ICCSHA256,Alpha:image.Alpha!=nil,Policy:"strip"}
   if mode=="manifest"{manifestDoc.Width++}
   manifestRaw,manifestRef:=contractBytes(t,manifestDoc)
   inputs:=[]v2input.Ref{imageRef,{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}}
   if image.Alpha!=nil{inputs=append(inputs,v2input.Ref{
    ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
   inputRaw,_:=canonical.Marshal(inputs)
   outputRaw,_:=canonical.Marshal([]v2input.Ref{encoded,manifestRef})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New(),Ref:encoded,
    Object:objectstore.Object{ContentType:"image/png"}}
   if mode=="mime"{ready.Object.ContentType="image/jpeg"}
   task:=CanonicalSigningTask{Task:ready.Task,Run:ready.Run,Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"export.encode",Version:"1",Params:paramsRef,
    InputDigest:strings.Repeat("c",64),ResultDigest:strings.Repeat("d",64),
    Inputs:inputRaw,Outputs:outputRaw,Metrics:json.RawMessage(`{}`)}
   docs:=delegatedDocumentsFunc(func(_ context.Context,_ CanonicalSigningTask,
    ref v2input.Ref,_ int)([]byte,error){
    switch ref{
    case paramsRef:return append([]byte(nil),params...),nil
    case imageRef:return append([]byte(nil),imageRaw...),nil
    case manifestRef:return append([]byte(nil),manifestRaw...),nil
    default:return nil,ErrInvalid
    }
   })
   calls:=0
   parent:=uuid.New()
   producer:=delegatedImageProducerFunc(func(_ context.Context,ref v2input.Ref)(uuid.UUID,error){
    calls++
    if ref!=imageRef{return uuid.Nil,ErrInvalid}
    if mode=="changed_parent"&&calls>1{return uuid.New(),nil}
    return parent,nil
   })
   result,err:=(DelegatedEncodeSigningTaskResolver{Documents:docs,
    Producers:producer}).ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||len(result.Node.Parents)!=1||result.Node.Parents[0]!=parent||
     calls!=2{t.Fatal("delegated encode claims lost",err)}
   }else if err==nil{t.Fatal("invalid delegated encode accepted",mode)}
  })
 }
}

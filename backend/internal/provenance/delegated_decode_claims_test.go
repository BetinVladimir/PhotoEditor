package provenance

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type delegatedOriginalFunc func(context.Context,CanonicalSigningTask,
 v2input.Ref,v2input.Ref,int64)error
func(f delegatedOriginalFunc)QualifyDelegatedOriginal(ctx context.Context,
 task CanonicalSigningTask,original,master v2input.Ref,size int64)error{
 return f(ctx,task,original,master,size)
}

func TestDelegatedDecodeClaimsRootAtPhysicalOriginal(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json")
 if err!=nil{t.Fatal(err)}
 var images []json.RawMessage
 if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing image")}
 for _,mode:=range []string{"valid","original","source_sha"}{
  t.Run(mode,func(t *testing.T){
   originalBody:=[]byte("physical encoded original")
   original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:bodyDigest(originalBody)}
   image,err:=review.ParseWorkingImage(images[0])
   if err!=nil{t.Fatal(err)}
   workICC:=image.Color.ICCSHA256
   image.Canvas.SourceSHA256=original.SHA256
   image.Canvas.SpaceID="oriented:"+original.SHA256
   image.Canvas.Orientation=1
   if mode=="source_sha"{image.Canvas.SourceSHA256=strings.Repeat("f",64)}
   imageRaw,master:=contractBytes(t,image)
   outputs,_:=canonical.Marshal([]v2input.Ref{
    {ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},master})
   paramsRaw,paramsRef:=contractBytes(t,signingDecodeParams{
    Source:original,Bytes:int64(len(originalBody)),Suffix:".png",Tile:512})
   inputs,_:=canonical.Marshal([]v2input.Ref{original})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New()}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),
    Result:uuid.New(),Attempt:1,Fence:1,Operation:"asset.decode",
    Version:"1",Params:paramsRef,Inputs:inputs,Outputs:outputs,
    Metrics:json.RawMessage(`{}`),InputDigest:strings.Repeat("c",64),
    ResultDigest:strings.Repeat("d",64)}
   docs:=delegatedDocumentsFunc(func(_ context.Context,_ CanonicalSigningTask,
    ref v2input.Ref,_ int)([]byte,error){
    switch ref{
    case paramsRef:return append([]byte(nil),paramsRaw...),nil
    case master:return append([]byte(nil),imageRaw...),nil
    default:return nil,ErrInvalid
    }
   })
   calls:=0
   originals:=delegatedOriginalFunc(func(_ context.Context,_ CanonicalSigningTask,
    gotOriginal,gotMaster v2input.Ref,size int64)error{
    calls++
    if mode=="original"||gotOriginal!=original||gotMaster!=master||
     size!=int64(len(originalBody)){return ErrInvalid}
    return nil
   })
   result,err:=(DelegatedDecodeSigningTaskResolver{Documents:docs,
    Originals:originals,WorkICCSHA:workICC}).
    ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||calls!=1||len(result.Sources)!=1||
     result.Sources[0]!=original||len(result.Node.Parents)!=0{
     t.Fatal("delegated original lineage lost",err)
    }
   }else if err==nil{t.Fatal("invalid delegated decode accepted",mode)}
  })
 }
}

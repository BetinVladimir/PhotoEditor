package provenance

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedMetadataClaimsBindPolicyAndParent(t *testing.T){
 for _,mode:=range []string{"valid","report","changed_parent"}{
  t.Run(mode,func(t *testing.T){
   encoded:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
   final:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
   before:=signingEncodedImage{Artifact:encoded,Format:"png",Width:8,
    Height:8,ICCID:"srgb_v1",ICCSHA:strings.Repeat("c",64),Policy:"strip"}
   beforeRaw,beforeRef:=contractBytes(t,before)
   author:="Author"
   documentRaw,documentRef:=contractBytes(t,signingMetadataDocument{Author:&author})
   paramsRaw,paramsRef:=contractBytes(t,signingMetadataParams{
    Encoded:beforeRef,Document:documentRef,Policy:"copyright_only",ToolID:"exiftool_v1"})
   after:=before
   after.Artifact=final
   after.Policy="copyright_only"
   afterRaw,afterRef:=contractBytes(t,after)
   report:=signingMetadataReport{Encoded:beforeRef,Document:documentRef,
    Policy:"copyright_only",Copied:[]string{"author"},Orientation:1,
    PreviewsRemoved:true,ICCPreserved:true}
   if mode=="report"{report.PreviewsRemoved=false}
   reportRaw,reportRef:=contractBytes(t,report)
   inputs,_:=canonical.Marshal([]v2input.Ref{beforeRef,encoded,documentRef})
   outputs,_:=canonical.Marshal([]v2input.Ref{final,afterRef,reportRef})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New(),Ref:final,
    Object:objectstore.Object{ContentType:"image/png"}}
   task:=CanonicalSigningTask{Task:ready.Task,Run:ready.Run,Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"metadata.write",Version:"1",Params:paramsRef,
    Inputs:inputs,Outputs:outputs,Metrics:json.RawMessage(`{}`),
    InputDigest:strings.Repeat("d",64),ResultDigest:strings.Repeat("e",64)}
   data:=map[v2input.Ref][]byte{paramsRef:paramsRaw,beforeRef:beforeRaw,
    documentRef:documentRaw,afterRef:afterRaw,reportRef:reportRaw}
   docs:=delegatedDocumentsFunc(func(_ context.Context,_ CanonicalSigningTask,
    ref v2input.Ref,_ int)([]byte,error){
    raw,ok:=data[ref]
    if !ok{return nil,ErrInvalid}
    return append([]byte(nil),raw...),nil
   })
   calls:=0
   parent:=uuid.New()
   producer:=delegatedImageProducerFunc(func(_ context.Context,
    ref v2input.Ref)(uuid.UUID,error){
    calls++
    if ref!=beforeRef{return uuid.Nil,ErrInvalid}
    if mode=="changed_parent"&&calls>1{return uuid.New(),nil}
    return parent,nil
   })
   result,err:=(DelegatedMetadataSigningTaskResolver{Documents:docs,
    Producers:producer}).ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||len(result.Node.Parents)!=1||result.Node.Parents[0]!=parent||
     calls!=2{t.Fatal("delegated metadata claim lost",err)}
   }else if err==nil{t.Fatal("invalid delegated metadata accepted",mode)}
  })
 }
}

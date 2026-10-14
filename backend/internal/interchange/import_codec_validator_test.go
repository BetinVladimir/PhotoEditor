package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importCodecChildrenFunc func(context.Context,v2input.Entry,[]byte)([]v2input.Entry,error)
func(f importCodecChildrenFunc)ImportDocumentChildren(ctx context.Context,e v2input.Entry,b []byte)([]v2input.Entry,error){return f(ctx,e,b)}
func TestImportCodecDocumentValidatorOwnsCallbacksAndRefusesInvalidBoundary(t *testing.T){
 for _,change:=range []string{"valid","missing_codec","missing_children","hash","unknown","cancel","extra_child"}{t.Run(change,func(t *testing.T){
  calls:=0;childCalls:=0
  raw:=[]byte(`{}`);entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  v:=ImportCodecDocumentValidator{Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},CanvasHash:rawSHA([]byte("canvas"))},Codec:importRouterCodec{kind:"Params",calls:&calls},Children:importCodecChildrenFunc(func(_ context.Context,_ v2input.Entry,b []byte)([]v2input.Entry,error){childCalls++;if string(b)!="{}"{t.Fatal("codec mutated child input")};b[0]='!';if change=="extra_child"{return []v2input.Entry{{Contract:"MaskRef",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))}}},nil};return nil,nil})}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch change{case "missing_codec":v.Codec=nil;case "missing_children":v.Children=nil;case "hash":entry.Ref.SHA256=rawSHA([]byte("other"));case "unknown":entry.Contract="Unknown";case "cancel":cancel()}
  children,err:=v.ImportDocumentChildren(ctx,entry,raw)
  if string(raw)!="{}"{t.Fatal("caller buffer changed")}
  if change=="valid"{if err!=nil||len(children)!=0||calls!=1||childCalls!=1{t.Fatalf("valid boundary %v",err)}}else{if err==nil{t.Fatal("invalid boundary accepted")};if change=="cancel"&&!errors.Is(err,context.Canceled){t.Fatal(err)};if change!="extra_child"&&(calls!=0||childCalls!=0){t.Fatal("refused boundary invoked callbacks")}}
 })}
}

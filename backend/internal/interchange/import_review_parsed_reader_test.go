package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type reviewParsedSourceStub struct{ref v2input.Ref;object objectstore.Object;calls int;change bool}
func(s *reviewParsedSourceStub)ResolveReviewParsed(context.Context)(v2input.Ref,objectstore.Object,error){s.calls++;object:=s.object;if s.change&&s.calls>1{object.VersionID="changed"};return s.ref,object,nil}
func TestReviewParsedReaderUsesValidatedCanonicalSourceAndRechecks(t *testing.T){
 for _,mode:=range []string{"valid","hash","changed","cancelled"}{t.Run(mode,func(t *testing.T){
  raw,err:=canonical.Marshal(importParsedValidatorFixture());if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  source:=&reviewParsedSourceStub{ref:ref,object:objectstore.Object{Key:"parsed",VersionID:"v1",SHA256:ref.SHA256,Bytes:int64(len(raw)),ContentType:"application/json"}}
  reader:=&importReaderStub{body:raw}
  if mode=="hash"{reader.body=append([]byte(nil),raw...);reader.body[0]='x'};source.change=mode=="changed"
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  got,err:=ReadImportReviewParsed(ctx,source,reader)
  if mode=="valid"{if err!=nil||string(got)!=string(raw)||source.calls!=2{t.Fatal("lost validated source",err)};return}
  if err==nil{t.Fatal("accepted substituted source")}
  if mode=="cancelled"&&source.calls!=0{t.Fatal("cancelled read resolved source")}
 })}
}

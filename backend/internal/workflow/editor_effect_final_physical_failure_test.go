package workflow

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "testing"

 "photoeditor/backend/internal/objectstore"
)

type editorPhysicalFixture struct{body []byte;openErr,readErr,closeErr error;opened objectstore.Object}
func(f *editorPhysicalFixture)Open(_ context.Context,o objectstore.Object)(io.ReadCloser,error){f.opened=o;if f.openErr!=nil{return nil,f.openErr};return &editorPhysicalFixtureReader{Reader:bytes.NewReader(f.body),readErr:f.readErr,closeErr:f.closeErr},nil}
func(f *editorPhysicalFixture)PutImmutable(context.Context,string,io.Reader,int64,string,string)(objectstore.Object,error){return objectstore.Object{},f.openErr}
func(f *editorPhysicalFixture)RecoverImmutable(context.Context,string,int64,string,string)(objectstore.Object,error){return objectstore.Object{},f.openErr}
type editorPhysicalFixtureReader struct{*bytes.Reader;readErr,closeErr error}
func(r *editorPhysicalFixtureReader)Read(p []byte)(int,error){if r.readErr!=nil{return 0,r.readErr};return r.Reader.Read(p)}
func(r *editorPhysicalFixtureReader)Close()error{return r.closeErr}

func TestEditorFinalPhysicalReadsClassifyOnlyBoundPermanentFailures(t *testing.T){
 body:=[]byte(`{"schema_version":1}`);sum:=sha256.Sum256(body)
 object:=objectstore.Object{Key:"immutable/input",VersionID:"frozen-v1",SHA256:hex.EncodeToString(sum[:]),Bytes:int64(len(body)),ContentType:"application/json"}
 transient:=errors.New("temporary transport failure")
 for _,tc:=range []struct{name string;fixture editorPhysicalFixture;code string;cause error}{
  {"exact",editorPhysicalFixture{body:body},"",nil},
  {"missing",editorPhysicalFixture{openErr:objectstore.ErrMissing},"EDITOR_SOURCE_OBJECT_MISSING",objectstore.ErrMissing},
  {"storage_integrity",editorPhysicalFixture{openErr:objectstore.ErrIntegrity},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"wrong_hash",editorPhysicalFixture{body:[]byte(`{"schema_version":2}`)},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"short",editorPhysicalFixture{body:body[:len(body)-1]},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"long",editorPhysicalFixture{body:append(append([]byte(nil),body...),0)},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"read_missing",editorPhysicalFixture{body:body,readErr:objectstore.ErrMissing},"EDITOR_SOURCE_OBJECT_MISSING",objectstore.ErrMissing},
  {"read_integrity",editorPhysicalFixture{body:body,readErr:objectstore.ErrIntegrity},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"close_integrity",editorPhysicalFixture{body:body,closeErr:objectstore.ErrIntegrity},"EDITOR_SOURCE_OBJECT_INTEGRITY",objectstore.ErrIntegrity},
  {"open_retry",editorPhysicalFixture{openErr:transient},"",transient},
  {"read_retry",editorPhysicalFixture{body:body,readErr:transient},"",transient},
  {"close_retry",editorPhysicalFixture{body:body,closeErr:transient},"",transient},
 }{t.Run(tc.name,func(t *testing.T){
  objects:=editorFinalPhysicalObjects{EditorEffectsObjects:&tc.fixture}
  reader,err:=objects.Open(context.Background(),object)
  if err==nil{_,err=io.ReadAll(reader);closeErr:=reader.Close();if err==nil{err=closeErr}}
  var physical *editorFinalPhysicalFailure
  if tc.code!=""{if !errors.As(err,&physical)||physical.Code!=tc.code||physical.Object!=object{t.Fatal("lost exact physical witness",err)}}else if errors.As(err,&physical){t.Fatal("transient error became terminal",err)}
  if tc.cause!=nil&&!errors.Is(err,tc.cause){t.Fatal("cause lost",err)}
  if tc.cause==nil&&err!=nil{t.Fatal(err)}
 })}
}

func TestEditorFinalPhysicalCancellationAndOutputErrorsRemainRetryable(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 fixture:=&editorPhysicalFixture{openErr:objectstore.ErrMissing}
 objects:=editorFinalPhysicalObjects{EditorEffectsObjects:fixture}
 if _,err:=objects.Open(ctx,objectstore.Object{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 for _,err:=range []error{context.Canceled,context.DeadlineExceeded,errors.New("contract rejected")}{
  if got:=editorFinalPhysicalError(objectstore.Object{},err);got!=err{t.Fatal("unbound control failure was terminalized")}
 }
 _,err:=objects.RecoverImmutable(context.Background(),"output",1,"application/json","hash")
 var physical *editorFinalPhysicalFailure
 if errors.As(err,&physical){t.Fatal("publication output failure became input loss")}
}

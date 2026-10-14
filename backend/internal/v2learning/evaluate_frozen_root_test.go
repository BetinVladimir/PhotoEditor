package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
 "bytes"
 "errors"
 "io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestReadFrozenRootDocumentRejectsChangedOrUnversionedBytes(t *testing.T) {
	body := []byte(`{"schema_version":1}`)
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "documents/recipe.json", VersionID: "v1",
		ContentType: "application/json", Bytes: int64(len(body)), SHA256: ref.SHA256}
	actual, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{body}, object, ref)
	if err != nil || string(actual) != string(body) {
		t.Fatalf("verified root rejected: %v", err)
	}
	if _, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{[]byte(`{"schema_version":2}`)}, object, ref); err == nil {
		t.Fatal("changed object bytes accepted")
	}
	object.VersionID = "null"
	if _, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{body}, object, ref); err == nil {
		t.Fatal("unversioned root accepted")
	}
}

type frozenRootLifecycleStream struct { io.Reader; closed *int; closeErr error; cancel context.CancelFunc }
func (s frozenRootLifecycleStream) Close()error{*s.closed++;if s.cancel!=nil{s.cancel()};return s.closeErr}
type frozenRootLifecycleReader struct { open func()(io.ReadCloser,error) }
func (r frozenRootLifecycleReader) Open(context.Context,objectstore.Object)(io.ReadCloser,error){return r.open()}
func TestFrozenRootReaderLifecycleRefusals(t *testing.T){
 for _,mode:=range []string{"cancel_before","nil_stream","stream_with_error","cancel_open","cancel_close","close_error"}{t.Run(mode,func(t *testing.T){
  body:=[]byte(`{"schema_version":1}`);sum:=sha256.Sum256(body)
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}
  object:=objectstore.Object{Key:"root",VersionID:"v1",ContentType:"application/json",Bytes:int64(len(body)),SHA256:ref.SHA256}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  opened,closed:=0,0
  sentinel:=errors.New("object failure")
  reader:=frozenRootLifecycleReader{open:func()(io.ReadCloser,error){
   opened++
   if mode=="nil_stream"{return nil,nil}
   stream:=frozenRootLifecycleStream{Reader:bytes.NewReader(body),closed:&closed}
   if mode=="close_error"{stream.closeErr=sentinel}
   if mode=="cancel_close"{stream.cancel=cancel}
   if mode=="cancel_open"{cancel()}
   if mode=="stream_with_error"{return stream,sentinel}
   return stream,nil
  }}
  if mode=="cancel_before"{cancel()}
  result,err:=ReadFrozenRootDocument(ctx,reader,object,ref)
  if err==nil||result!=nil{t.Fatalf("refusal returned bytes: %v",err)}
  expectedOpen,expectedClose:=1,1
  if mode=="cancel_before"{expectedOpen,expectedClose=0,0}
  if mode=="nil_stream"{expectedClose=0}
  if opened!=expectedOpen||closed!=expectedClose{t.Fatalf("open=%d close=%d",opened,closed)}
  if (mode=="stream_with_error"||mode=="close_error")&&!errors.Is(err,sentinel){t.Fatal("object failure lost")}
  if (mode=="cancel_before"||mode=="cancel_open"||mode=="cancel_close")&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost")}
 })}
}

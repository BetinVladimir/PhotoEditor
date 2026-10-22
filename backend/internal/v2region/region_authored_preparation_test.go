package v2region

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type authoredIOStream struct{io.Reader;closed bool;closeErr error}
func(s *authoredIOStream)Close()error{s.closed=true;return s.closeErr}
type authoredIOObjects struct{stream io.ReadCloser;err error;opened objectstore.Object;calls int}
func(o *authoredIOObjects)Open(_ context.Context,value objectstore.Object)(io.ReadCloser,error){o.calls++;o.opened=value;return o.stream,o.err}
type authoredFailReader struct{err error}
func(r authoredFailReader)Read([]byte)(int,error){return 0,r.err}

func TestAuthoredRegionPhysicalDocumentsCloseStreamsAndPreserveOriginalIOFailure(t *testing.T){
 raw:=[]byte(`{"full_authored_root":true}`);sum:=sha256.Sum256(raw);sha:=hex.EncodeToString(sum[:])
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 object:=objectstore.Object{Key:"exact-document",VersionID:"immutable-v1",ContentType:"application/json",Bytes:int64(len(raw)),SHA256:sha}
 opened:=errors.New("open failure");readFailure:=errors.New("read failure");closeFailure:=errors.New("close failure")
 for _,name:=range []string{"valid","nil-stream","open-and-stream","read-and-close","close","changed","overlong"}{t.Run(name,func(t *testing.T){
  stream:=&authoredIOStream{Reader:bytes.NewReader(raw)};objects:=&authoredIOObjects{stream:stream};expected:=error(nil)
  switch name{
  case "nil-stream":objects.stream=nil;expected=ErrRegionInvalid
  case "open-and-stream":objects.err=opened;stream.closeErr=closeFailure;expected=opened
  case "read-and-close":stream.Reader=authoredFailReader{readFailure};stream.closeErr=closeFailure;expected=readFailure
  case "close":stream.closeErr=closeFailure;expected=closeFailure
  case "changed":other:=append([]byte(nil),raw...);other[2]='X';stream.Reader=bytes.NewReader(other);expected=ErrRegionInvalid
  case "overlong":stream.Reader=bytes.NewReader(append(append([]byte(nil),raw...),byte(' ')));expected=ErrRegionInvalid
  }
  body,err:=readAuthoredRegionDocument(context.Background(),objects,object,ref)
  if !errors.Is(err,expected)||expected==nil&&!bytes.Equal(body,raw){t.Fatal(name,err)}
  if objects.opened.VersionID!=object.VersionID||objects.opened.Key!=object.Key||objects.stream!=nil&&!stream.closed{t.Fatal("lost version or stream ownership")}
 })}
 for _,version:=range []string{"","null"}{invalid:=object;invalid.VersionID=version;objects:=&authoredIOObjects{}
  if _,err:=readAuthoredRegionDocument(context.Background(),objects,invalid,ref);!errors.Is(err,ErrRegionInvalid)||objects.calls!=0{t.Fatal("unversioned document performed IO",err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel();objects:=&authoredIOObjects{}
 if _,err:=readAuthoredRegionDocument(ctx,objects,object,ref);!errors.Is(err,context.Canceled)||objects.calls!=0{t.Fatal("cancelled document performed IO",err)}
}

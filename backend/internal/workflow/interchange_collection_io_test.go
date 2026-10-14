package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

type collectionFaultReader struct{body io.Reader;readErr,closeErr error;reads,closes int;onRead func()}
func(r *collectionFaultReader)Read(b []byte)(int,error){r.reads++;if r.onRead!=nil{r.onRead()};if r.readErr!=nil{return 0,r.readErr};return r.body.Read(b)}
func(r *collectionFaultReader)Close()error{r.closes++;return r.closeErr}
type collectionFaultPorts struct{candidate *InterchangeCollectionCandidate;reader io.ReadCloser;openErr error;onOpen func();prepared bool;received InterchangeCollectionCandidate}
func(p *collectionFaultPorts)Next(context.Context)(*InterchangeCollectionCandidate,error){return p.candidate,nil}
func(p *collectionFaultPorts)Open(context.Context,objectstore.Object)(io.ReadCloser,error){if p.onOpen!=nil{p.onOpen()};return p.reader,p.openErr}
func(p *collectionFaultPorts)Prepare(_ context.Context,c InterchangeCollectionCandidate,_ []byte)(bool,error){p.prepared=true;p.received=c;return true,nil}
func TestInterchangeCollectionObjectIOFailureAndOwnedCandidate(t *testing.T){
 for _,mode:=range []string{"nil_reader","open_error_reader","read_error","close_error","cancel_open","cancel_read","candidate_mutated","noncanonical","invalid_digest"}{t.Run(mode,func(t *testing.T){
  body:=`{"schema_version":1}`
  if mode=="noncanonical"{body="{ \"schema_version\": 1 }"}
  sum:=sha256.Sum256([]byte(body))
  candidate:=&InterchangeCollectionCandidate{Tenant:uuid.New(),Workflow:uuid.New(),Artifact:uuid.New(),SHA:hex.EncodeToString(sum[:]),Bytes:int64(len(body)),Bucket:"photos",Key:"pinned",Version:"v1"}
  expected:=*candidate;sentinel:=errors.New("object IO refused")
  reader:=&collectionFaultReader{body:strings.NewReader(body)}
  ports:=&collectionFaultPorts{candidate:candidate,reader:reader}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{
  case "nil_reader":ports.reader=nil
  case "open_error_reader":ports.openErr=sentinel
  case "read_error":reader.readErr=sentinel
  case "close_error":reader.closeErr=sentinel
  case "cancel_open":ports.onOpen=cancel
  case "cancel_read":reader.onRead=cancel
  case "candidate_mutated":ports.onOpen=func(){candidate.Workflow=uuid.New();candidate.Tenant=uuid.New();candidate.SHA=strings.Repeat("f",64);candidate.Bytes=1}
  case "invalid_digest":candidate.SHA=strings.Repeat("F",64)
  }
  done,err:=(InterchangeCollectionProcessor{Store:ports,Objects:ports,Bucket:"photos"}).ProcessOne(ctx)
  if mode=="candidate_mutated"{
   if !done||err!=nil||!ports.prepared||ports.received!=expected{t.Fatal("candidate not frozen before IO",done,err)}
  }else{
   if done||err==nil||ports.prepared{t.Fatal("failed source prepared collection",mode,done,err)}
   if mode=="open_error_reader"||mode=="read_error"||mode=="close_error"{if !errors.Is(err,sentinel){t.Fatal("IO error lost",err)}}
   if mode=="cancel_open"||mode=="cancel_read"{if !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}}
  }
  if mode!="nil_reader"&&mode!="invalid_digest"&&reader.closes!=1{t.Fatal("reader not closed exactly once",reader.closes)}
  if (mode=="open_error_reader"||mode=="cancel_open"||mode=="invalid_digest")&&reader.reads!=0{t.Fatal("refused source read",reader.reads)}
 })}
}
func TestSQLInterchangeCollectionStoreMissingDatabaseAndCancellation(t *testing.T){
 store:=SQLInterchangeCollectionStore{}
 if _,err:=store.Next(context.Background());err==nil{t.Fatal("missing DB accepted")}
 if _,err:=store.Prepare(context.Background(),InterchangeCollectionCandidate{},nil);err==nil{t.Fatal("missing DB accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=store.Next(ctx);!errors.Is(err,context.Canceled){t.Fatal("Next lost cancellation",err)}
 if _,err:=store.Prepare(ctx,InterchangeCollectionCandidate{},nil);!errors.Is(err,context.Canceled){t.Fatal("Prepare lost cancellation",err)}
}

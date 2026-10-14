package httpapi

import (
 "context"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/interchange"
)

type archiveAttemptHTTPStub struct{ calls int; value interchange.ArchiveUploadStatus }
func(s *archiveAttemptHTTPStub)Status(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(interchange.ArchiveUploadStatus,error){return interchange.ArchiveUploadStatus{},nil}
func(s *archiveAttemptHTTPStub)AttemptStatus(_ context.Context,_ auth.Identity,_ uuid.UUID,_ string,_ interchange.ArchiveUploadRequest)(interchange.ArchiveUploadStatus,error){s.calls++;return s.value,nil}

func TestArchiveAttemptStatusIsReadOnlyAndBindsDerivedIdentity(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,pid:=uuid.New(),uuid.New();sha:=strings.Repeat("a",64)
 body:=`{"filename":"archive.zip","bytes":22,"sha256":"`+sha+`"}`
 intent,err:=interchange.FreezeArchiveUpload(context.Background(),tid,who,"saved-key",interchange.ArchiveUploadRequest{ProjectID:pid,Filename:"archive.zip",Bytes:22,SHA256:sha});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","missing-key","foreign-upload","unknown-field"}{t.Run(mode,func(t *testing.T){
  stub:=&archiveAttemptHTTPStub{value:interchange.ArchiveUploadStatus{UploadID:intent.ID(),State:"reserved",RequestSHA:intent.RequestSHA(),ExpiresAt:time.Now().Add(time.Hour),BeginRecovery:"reconciliation_required"}}
  key,payload:= "saved-key",body;want:=200
  switch mode{case "missing-key":key="";want=422;case "foreign-upload":stub.value.UploadID=uuid.New();want=503;case "unknown-field":payload=`{"project_id":"foreign"}`;want=400}
  r:=httptest.NewRequest("POST","/",strings.NewReader(payload));r.Header.Set("Idempotency-Key",key);w:=httptest.NewRecorder()
  serveInterchangeArchiveAttemptStatus(w,r,who,tid,pid,stub)
  if w.Code!=want{t.Fatal(mode,w.Code,w.Body.String())}
  if want==200&&(!strings.Contains(w.Body.String(),"reconciliation_required")||w.Header().Get("Cache-Control")!="no-store"){t.Fatal("status missing or cacheable")}
  if want==422||want==400{if stub.calls!=0{t.Fatal("invalid attempt reached reader")}}
 })}
}

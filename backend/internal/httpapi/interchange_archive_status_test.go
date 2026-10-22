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
 "photoeditor/backend/internal/v2input"
)
type archiveStatusHTTPStub struct{value interchange.ArchiveUploadStatus;calls int;cancel context.CancelFunc}
func TestArchiveStatusAbortDuringUnresolvedBegin(t *testing.T){
 upload:=uuid.New();sha:=strings.Repeat("a",64)
 status:=interchange.ArchiveUploadStatus{UploadID:upload,State:"aborting",RequestSHA:sha,ExpiresAt:time.Now().Add(time.Hour),ErrorCode:"UPLOAD_ABORTED",BeginRecovery:"reconciliation_required"}
 if !validInterchangeArchiveStatus(status,upload,sha){t.Fatal("actual abort/creating generation status rejected")}
 status.BeginRecovery="receipt_pending_bind"
 if validInterchangeArchiveStatus(status,upload,sha){t.Fatal("aborting generation exposed a bind action")}
}
func(s *archiveStatusHTTPStub)Status(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(interchange.ArchiveUploadStatus,error){s.calls++;if s.cancel!=nil{s.cancel()};return s.value,nil}
func TestArchiveHTTPStatusValidatesProviderAndReadiness(t *testing.T){
 for _,mode:=range []string{"pending","verified-not-ready","ready","aborting","foreign-upload","foreign-hash","bad-state","bad-error","active-with-error","aborting-without-error","missing-expiry","missing-artifact","foreign-artifact","bad-artifact-sha","pending-artifact","ready-completing","uninstalled","cancelled"}{t.Run(mode,func(t *testing.T){
  upload:=uuid.New();sha:=strings.Repeat("a",64);stub:=&archiveStatusHTTPStub{value:interchange.ArchiveUploadStatus{UploadID:upload,State:"completing",RequestSHA:sha,ExpiresAt:time.Now().Add(time.Hour)}}
  expected:=200;var provider InterchangeArchiveStatusReader=stub
  switch mode{
  case "verified-not-ready":stub.value.State="verified"
  case "aborting":stub.value.State="aborting";stub.value.ErrorCode="UPLOAD_ABORTED"
  case "ready","missing-artifact","foreign-artifact","bad-artifact-sha","ready-completing":
   stub.value.State="verified";stub.value.Ready=true;stub.value.Artifact=&v2input.Ref{ArtifactID:uuid.NewSHA1(upload,[]byte("archive:artifact:1")),SHA256:sha}
  }
  switch mode{
  case "foreign-upload":stub.value.UploadID=uuid.New();expected=503
  case "foreign-hash":stub.value.RequestSHA=strings.Repeat("b",64);expected=503
  case "bad-state":stub.value.State="unknown";expected=503
  case "bad-error":stub.value.ErrorCode="internal-secret";expected=503
  case "active-with-error":stub.value.ErrorCode="UPLOAD_ABORTED";expected=503
  case "aborting-without-error":stub.value.State="aborting";expected=503
  case "missing-expiry":stub.value.ExpiresAt=time.Time{};expected=503
  case "missing-artifact":stub.value.Artifact=nil;expected=503
  case "foreign-artifact":stub.value.Artifact.ArtifactID=uuid.New();expected=503
  case "bad-artifact-sha":stub.value.Artifact.SHA256="bad";expected=503
  case "pending-artifact":stub.value.Artifact=&v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};expected=503
  case "ready-completing":stub.value.State="completing";expected=503
  case "uninstalled":provider=nil;expected=503
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{stub.cancel=cancel}
  r:=httptest.NewRequest("GET","/?request_sha256="+sha,nil).WithContext(ctx);w:=httptest.NewRecorder()
  serveInterchangeArchiveStatus(w,r,auth.Identity{},uuid.New(),uuid.New(),upload,provider)
  if mode=="cancelled"{if w.Body.Len()!=0{t.Fatal("response after cancellation")};return}
  if w.Code!=expected{t.Fatal(w.Code,w.Body.String())}
  if expected==200&&w.Header().Get("Cache-Control")!="no-store"{t.Fatal("cacheable scoped status")}
  if mode=="verified-not-ready"&&strings.Contains(w.Body.String(),"artifact"){t.Fatal("ledger state exposed artifact")}
  if mode=="bad-error"&&strings.Contains(w.Body.String(),"internal-secret"){t.Fatal("provider error leaked")}
 })}
}
func TestArchiveHTTPStatusRejectsMissingDuplicateAndInvalidSHA(t *testing.T){
 sha:=strings.Repeat("a",64)
 for _,query:=range []string{"","request_sha256=bad","request_sha256="+sha+"&request_sha256="+sha}{
  stub:=&archiveStatusHTTPStub{};r:=httptest.NewRequest("GET","/?"+query,nil);w:=httptest.NewRecorder()
  serveInterchangeArchiveStatus(w,r,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),stub)
  if w.Code!=422||stub.calls!=0{t.Fatal("invalid query reached provider",w.Code,stub.calls)}
 }
}

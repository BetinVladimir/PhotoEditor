package httpapi
import (
 "context"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)
type archiveActionsHTTPStub struct{parts,complete,abort int;corrupt bool;unsafeURL string}
func(s *archiveActionsHTTPStub)Parts(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string,[]int)([]objectstore.PartURL,error){s.parts++;n:=1;if s.corrupt{n=2};link:="https://storage.example/signed";if s.unsafeURL!=""{link=s.unsafeURL};return []objectstore.PartURL{{PartNumber:n,URL:link,ExpiresAt:time.Now().Add(time.Minute)}},nil}
func(s *archiveActionsHTTPStub)Complete(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string,[]objectstore.UploadedPart)error{s.complete++;return nil}
func TestArchiveHTTPActionsValidationAndAcceptedSemantics(t *testing.T){
 for _,mode:=range []string{"parts","corrupt","duplicate","complete","invalid-sha","uninstalled"}{t.Run(mode,func(t *testing.T){
  stub:=&archiveActionsHTTPStub{corrupt:mode=="corrupt"};var service InterchangeArchiveActions=stub
  body:=`{"request_sha256":"`+strings.Repeat("a",64)+`","parts":[1]}`;status:=200
  switch mode{case "corrupt":status=503;case "duplicate":body=`{"request_sha256":"`+strings.Repeat("a",64)+`","parts":[1,1]}`;status=422;case "complete":body=`{"request_sha256":"`+strings.Repeat("a",64)+`","parts":[{"part_no":1,"etag":"receipt"}]}`;status=202;case "invalid-sha":body=`{"request_sha256":"bad","parts":[1]}`;status=422;case "uninstalled":service=nil;status=503}
  r:=httptest.NewRequest("POST","/",strings.NewReader(body));r.Header.Set("Content-Type","application/json");w:=httptest.NewRecorder()
  if mode=="complete"{serveInterchangeArchiveComplete(w,r,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),service)}else{serveInterchangeArchiveParts(w,r,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),service)}
  if w.Code!=status{t.Fatalf("status=%d body=%s",w.Code,w.Body.String())}
  if status==422&&stub.parts!=0{t.Fatal("invalid request reached signer")}
  if mode=="complete"&&(stub.complete!=1||strings.Contains(w.Body.String(),"verified")){t.Fatal("false verification acknowledgement")}
 })}
}

func(s *archiveActionsHTTPStub)Abort(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(bool,error){s.abort++;return !s.corrupt,nil}
func TestArchiveHTTPAbortAcknowledgesCleanupPending(t *testing.T){
 for _,mode:=range []string{"ok","already-cleaned","invalid","uninstalled"}{t.Run(mode,func(t *testing.T){
  stub:=&archiveActionsHTTPStub{corrupt:mode=="already-cleaned"};var service InterchangeArchiveActions=stub
  body:=`{"request_sha256":"`+strings.Repeat("a",64)+`"}`;status:=202
  if mode=="invalid"{body=`{"request_sha256":"bad"}`;status=422};if mode=="uninstalled"{service=nil;status=503}
  r:=httptest.NewRequest("POST","/",strings.NewReader(body));r.Header.Set("Content-Type","application/json");w:=httptest.NewRecorder()
  serveInterchangeArchiveAbort(w,r,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),service)
  if w.Code!=status{t.Fatal(w.Code,w.Body.String())}
  if mode=="ok"||mode=="already-cleaned"{expected:=`"cleanup_pending":true`;if mode=="already-cleaned"{expected=`"cleanup_pending":false`};if stub.abort!=1||!strings.Contains(w.Body.String(),expected){t.Fatal("wrong purge acknowledgement")}}else if stub.abort!=0{t.Fatal("invalid abort reached service")}
 })}
}

func TestArchiveHTTPPartsRejectUnsafeProviderLinks(t *testing.T){
 for _,link:=range []string{"javascript:alert(1)","https://user:secret@storage.example/a","https://storage.example/a#fragment","https:///a"}{
  stub:=&archiveActionsHTTPStub{unsafeURL:link};body:=`{"request_sha256":"`+strings.Repeat("a",64)+`","parts":[1]}`
  r:=httptest.NewRequest("POST","/",strings.NewReader(body));r.Header.Set("Content-Type","application/json");w:=httptest.NewRecorder()
  serveInterchangeArchiveParts(w,r,auth.Identity{},uuid.New(),uuid.New(),uuid.New(),stub)
  if w.Code!=503||strings.Contains(w.Body.String(),link){t.Fatal("unsafe provider link exposed",w.Code)}
 }
}

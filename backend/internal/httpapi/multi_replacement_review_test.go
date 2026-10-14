package httpapi
import (
 "context"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2render"
)
type multiReplacementReaderStub struct{
 MultiProposalReader
 calls int
 who auth.Identity
 tenant,project,review,original,after uuid.UUID
 hash,operation string
 err error
}
func(s *multiReplacementReaderStub)ReplacementReviewScope(_ context.Context,who auth.Identity,tenant,project,review,original uuid.UUID,hash,operation string)(v2render.MultiReplacementReviewScope,error){
 s.calls++;s.who=who;s.tenant=tenant;s.project=project;s.review=review;s.original=original;s.hash=hash;s.operation=operation
 return v2render.MultiReplacementReviewScope{Tenant:tenant,Project:project,Review:review,OriginalTask:original,DecisionHash:hash,Operation:operation},s.err
}
func(s *multiReplacementReaderStub)ReplacementReviewPage(_ context.Context,who auth.Identity,tenant,review uuid.UUID,hash string,after uuid.UUID)(v2render.MultiReplacementReviewPage,error){
 s.calls++;s.who=who;s.tenant=tenant;s.review=review;s.hash=hash;s.after=after
 return v2render.MultiReplacementReviewPage{Review:review,DecisionHash:hash,Items:[]v2render.MultiReplacementReviewItem{}},s.err
}
func TestMultiReplacementHTTPQueryContract(t *testing.T){
 for _,list:=range []bool{false,true}{
  cases:=[]struct{query string;status int}{
   {"decision_hash="+strings.Repeat("a",64),200},
   {"",422},{"decision_hash=bad",422},{"decision_hash="+strings.Repeat("a",64)+"&decision_hash="+strings.Repeat("a",64),422},
  }
  if list{cases=append(cases,struct{query string;status int}{"decision_hash="+strings.Repeat("a",64)+"&after=bad",422})}
  for _,c:=range cases{
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project,review,original:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
   stub:=&multiReplacementReaderStub{};s:=&Server{MultiProposals:stub};router:=chi.NewRouter()
   path:="/projects/"+project.String()+"/reviews/"+review.String()+"/multi-replacements/"+original.String()
   query:=c.query
   if list{path="/reviews/"+review.String()+"/multi-replacements";router.Get("/reviews/{review}/multi-replacements",func(w http.ResponseWriter,r *http.Request){s.serveMultiReplacementPage(w,r,who,tenant)})
   }else{query+="&operation=optical.focus_stack";router.Get("/projects/{id}/reviews/{review}/multi-replacements/{original}",func(w http.ResponseWriter,r *http.Request){s.serveMultiReplacementScope(w,r,who,tenant)})}
   out:=httptest.NewRecorder();router.ServeHTTP(out,httptest.NewRequest("GET",path+"?"+query,nil))
   if out.Code!=c.status||out.Header().Get("Cache-Control")!="no-store"{t.Fatal(list,query,out.Code,out.Body.String())}
   if c.status==200{if stub.calls!=1||stub.who!=who||stub.tenant!=tenant||stub.review!=review||stub.hash!=strings.Repeat("a",64){t.Fatal("request authority changed")};if !list&&(stub.operation!="optical.focus_stack"||stub.project!=project||stub.original!=original){t.Fatal("replacement scope changed")}
   }else if stub.calls!=0{t.Fatal("invalid query reached service")}
  }
 }
}
func TestMultiReplacementHTTPAuthNoStore(t *testing.T){
 s:=&Server{Auth:&auth.Service{Pepper:[]byte("multi-replacement-auth")}}
 for _,path:=range []string{"/reviews/"+uuid.NewString()+"/multi-replacements","/projects/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/multi-replacements/"+uuid.NewString()}{
  out:=httptest.NewRecorder();s.Routes().ServeHTTP(out,httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+path,nil))
  if out.Code!=401||out.Header().Get("Cache-Control")!="no-store"{t.Fatal(out.Code,out.Header())}
 }
}
func TestMultiReplacementHTTPServiceFailureAndOperation(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack","optical.perspective","","optical.panorama&operation=optical.focus_stack"}{
  stub:=&multiReplacementReaderStub{err:v2input.ErrExpired};s:=&Server{MultiProposals:stub};router:=chi.NewRouter()
  router.Get("/{id}/{review}/{original}",func(w http.ResponseWriter,r *http.Request){s.serveMultiReplacementScope(w,r,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New())})
  out:=httptest.NewRecorder();router.ServeHTTP(out,httptest.NewRequest("GET","/"+uuid.NewString()+"/"+uuid.NewString()+"/"+uuid.NewString()+"?decision_hash="+strings.Repeat("a",64)+"&operation="+operation,nil))
  valid:=operation=="optical.panorama"||operation=="optical.focus_stack"
  if valid{if out.Code==200||stub.calls!=1{t.Fatal("unavailable scope accepted")}}else if out.Code!=422||stub.calls!=0{t.Fatal("invalid operation accepted")}
 }
}

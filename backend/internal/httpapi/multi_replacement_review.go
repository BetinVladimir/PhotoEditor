package httpapi
import (
 "context"
 "net/http"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2render"
)
type MultiReplacementPageReader interface {
 ReplacementReviewPage(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,uuid.UUID)(v2render.MultiReplacementReviewPage,error)
}
type MultiReplacementScopeReader interface {
 ReplacementReviewScope(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,string,string)(v2render.MultiReplacementReviewScope,error)
}
func(s *Server) multiReplacementReviewRoutes(router chi.Router){
 router.Get("/api/v1/tenants/{t}/reviews/{review}/multi-replacements",func(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Cache-Control","no-store")
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  s.serveMultiReplacementPage(w,r,who,tenant)
 })

 router.Get("/api/v1/tenants/{t}/projects/{id}/reviews/{review}/multi-replacements/{original}",func(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Cache-Control","no-store")
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  s.serveMultiReplacementScope(w,r,who,tenant)
 })
}

func(s *Server) serveMultiReplacementPage(w http.ResponseWriter,r *http.Request,who auth.Identity,tenant uuid.UUID){
 w.Header().Set("Cache-Control","no-store")
  var err error
  review,e:=uuid.Parse(chi.URLParam(r,"review"));hashes:=r.URL.Query()["decision_hash"];cursors:=r.URL.Query()["after"]
  var after uuid.UUID
  valid:=e==nil&&review!=uuid.Nil&&len(hashes)==1&&len(cursors)<=1
  if len(cursors)==1 { after,e=uuid.Parse(cursors[0]);valid=valid&&e==nil&&after!=uuid.Nil }
  if valid { _,e=domain.ParseDigest(hashes[0]);valid=e==nil }
  if !valid { writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return }
  reader,ok:=s.MultiProposals.(MultiReplacementPageReader)
  if !ok { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  page,err:=reader.ReplacementReviewPage(r.Context(),who,tenant,review,hashes[0],after);if err!=nil { writeError(w,r,semanticProposalError(err));return }
  w.Header().Set("Cache-Control","no-store");writeJSON(w,200,page)
}
func(s *Server) serveMultiReplacementScope(w http.ResponseWriter,r *http.Request,who auth.Identity,tenant uuid.UUID){
 w.Header().Set("Cache-Control","no-store")
  var err error
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));review,e2:=uuid.Parse(chi.URLParam(r,"review"));original,e3:=uuid.Parse(chi.URLParam(r,"original"))
  hashes:=r.URL.Query()["decision_hash"]
  if e1!=nil||e2!=nil||e3!=nil||project==uuid.Nil||review==uuid.Nil||original==uuid.Nil||len(hashes)!=1 {
   writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return
  }
  if _,err=domain.ParseDigest(hashes[0]);err!=nil { writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return }
  operations:=r.URL.Query()["operation"]
  if len(operations)!=1||(operations[0]!="optical.panorama"&&operations[0]!="optical.focus_stack"){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  reader,ok:=s.MultiProposals.(MultiReplacementScopeReader)
  if !ok { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  scope,err:=reader.ReplacementReviewScope(r.Context(),who,tenant,project,review,original,hashes[0],operations[0]);if err!=nil { writeError(w,r,semanticProposalError(err));return }
  w.Header().Set("Cache-Control","no-store");writeJSON(w,200,scope)
}

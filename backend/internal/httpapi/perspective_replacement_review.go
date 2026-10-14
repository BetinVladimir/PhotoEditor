package httpapi

import (
 "net/http"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)

func(s *Server) perspectiveReplacementReviewRoutes(router chi.Router) {
 router.Get("/api/v1/tenants/{t}/reviews/{review}/perspective-replacements",func(w http.ResponseWriter,r *http.Request) {
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil { writeError(w,r,err);return }
  review,e:=uuid.Parse(chi.URLParam(r,"review"));hashes:=r.URL.Query()["decision_hash"];cursors:=r.URL.Query()["after"]
  var after uuid.UUID
  valid:=e==nil&&review!=uuid.Nil&&len(hashes)==1&&len(cursors)<=1
  if len(cursors)==1 { after,e=uuid.Parse(cursors[0]);valid=valid&&e==nil&&after!=uuid.Nil }
  if valid { _,e=domain.ParseDigest(hashes[0]);valid=e==nil }
  if !valid { writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return }
  if s.PerspectiveProposals==nil { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  page,err:=s.PerspectiveProposals.ReplacementReviewPage(r.Context(),who,tenant,review,hashes[0],after);if err!=nil { writeError(w,r,semanticProposalError(err));return }
  w.Header().Set("Cache-Control","no-store");writeJSON(w,200,page)
 })
 router.Get("/api/v1/tenants/{t}/projects/{id}/reviews/{review}/perspective-replacements/{original}",func(w http.ResponseWriter,r *http.Request) {
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil { writeError(w,r,err);return }
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));review,e2:=uuid.Parse(chi.URLParam(r,"review"));original,e3:=uuid.Parse(chi.URLParam(r,"original"))
  hashes:=r.URL.Query()["decision_hash"]
  if e1!=nil||e2!=nil||e3!=nil||project==uuid.Nil||review==uuid.Nil||original==uuid.Nil||len(hashes)!=1 {
   writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return
  }
  if _,err=domain.ParseDigest(hashes[0]);err!=nil { writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return }
  if s.PerspectiveProposals==nil { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  scope,err:=s.PerspectiveProposals.ReplacementReviewScope(r.Context(),who,tenant,project,review,original,hashes[0]);if err!=nil { writeError(w,r,semanticProposalError(err));return }
  w.Header().Set("Cache-Control","no-store");writeJSON(w,200,scope)
 })
}

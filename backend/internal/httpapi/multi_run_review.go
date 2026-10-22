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
type MultiRunReviewReader interface {RunReviewScope(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(*v2render.MultiRunReviewScope,error)}
func(s *Server) multiRunReviewRoutes(router chi.Router){
 router.Get("/api/v1/tenants/{t}/runs/{run}/multi-review",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  who,t,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  run,err:=uuid.Parse(chi.URLParam(r,"run"));if err!=nil||run==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  reader,ok:=s.MultiProposals.(MultiRunReviewReader);if !ok{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  value,err:=reader.RunReviewScope(r.Context(),who,t,run);if err!=nil{writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return}
  writeJSON(w,200,struct{Scope *v2render.MultiRunReviewScope `json:"scope"`}{value})
 })
}

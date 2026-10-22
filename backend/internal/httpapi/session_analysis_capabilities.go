package httpapi

import (
 "net/http"
 "github.com/go-chi/chi/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2sessions"
)

func(s *Server) sessionAnalysisCapabilityRoutes(router chi.Router,sessions v2sessions.Service){
 router.Post("/api/v1/tenants/{t}/shoot-sessions/{id}/analysis-capabilities",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","private, no-store")
  who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  if r.URL.RawQuery!=""{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  var req v2sessions.AnalysisRequest;if err=decodeJSON(r,&req);err!=nil{writeError(w,r,err);return}
  out,err:=sessions.AnalysisCapabilities(r.Context(),who,tid,id,req);if err!=nil{writeError(w,r,err);return}
  if r.Context().Err()!=nil{return};writeJSON(w,200,out)
 })
}

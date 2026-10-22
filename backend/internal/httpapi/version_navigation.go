package httpapi

import (
 "context"
 "net/http"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/asset"
 "photoeditor/backend/internal/auth"
)
type versionNavigationReader interface {GetVersionNavigation(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(asset.VersionNavigation,error)}
func(s *Server)versionNavigationRoutes(router chi.Router,reader versionNavigationReader){
 router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/navigation",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  pid,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  version,err:=routeID(r,"version");if err!=nil{writeError(w,r,err);return}
  out,err:=reader.GetVersionNavigation(r.Context(),who,tid,pid,version);if err!=nil{writeError(w,r,err);return}
  if err=r.Context().Err();err!=nil{writeError(w,r,err);return}
  w.Header().Set("Cache-Control","no-store");writeJSON(w,http.StatusOK,out)
 })
}

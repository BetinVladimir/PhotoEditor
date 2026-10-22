package httpapi

import (
 "net/http"
 "github.com/go-chi/chi/v5"
 collectionservice "photoeditor/backend/internal/collection"
)
func (s Server) shareUsageRoutes(r chi.Router,collections collectionservice.Service){
 r.Get("/api/v1/tenants/{t}/collections/{id}/shares/{share}/usage",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  share,err:=routeID(r,"share");if err!=nil{writeError(w,r,err);return}
  out,err:=collections.ShareUsage(r.Context(),who,tid,id,share);if err!=nil{writeError(w,r,err);return}
  w.Header().Set("Cache-Control","no-store");writeJSON(w,200,out)
 })
}

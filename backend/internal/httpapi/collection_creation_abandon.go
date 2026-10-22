package httpapi

import (
 "net/http"
 "github.com/go-chi/chi/v5"
 collectionservice "photoeditor/backend/internal/collection"
)
func(s Server)collectionCreationAbandonRoutes(r chi.Router,collections collectionservice.Service){
 r.Post("/api/v1/tenants/{t}/collections/abandon",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  var req collectionservice.Create;if err=decodeJSON(r,&req);err!=nil{writeError(w,r,err);return}
  out,err:=collections.AbandonCreation(r.Context(),who,tid,r.Header.Get("Idempotency-Key"),req);if err!=nil{writeError(w,r,err);return}
  w.Header().Set("Cache-Control","no-store");w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
 })
 r.Post("/api/v1/tenants/{t}/collections/{id}/shares/abandon",func(w http.ResponseWriter,r *http.Request){
  who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  id,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  var req collectionservice.ShareCreate;if err=decodeJSON(r,&req);err!=nil{writeError(w,r,err);return}
  out,err:=collections.AbandonShareCreation(r.Context(),who,tid,id,r.Header.Get("Idempotency-Key"),req);if err!=nil{writeError(w,r,err);return}
  w.Header().Set("Cache-Control","no-store");w.Header().Set("Content-Type","application/json");w.WriteHeader(out.Status);_,_=w.Write(out.Body)
 })
}

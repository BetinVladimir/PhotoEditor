package httpapi
import (
 "net/http"
 "net/url"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)
func(s *Server) collectionCullingRoutes(r chi.Router){
 r.Get("/api/v1/tenants/{t}/collections/{id}/culling-groups",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","private, no-store")
  who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};id,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
  q,e:=url.ParseQuery(r.URL.RawQuery);if e!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  for key,values:=range q{if key!="cursor"&&key!="review_id"||len(values)!=1||values[0]==""{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}}
  if len(q.Get("cursor"))>2048||q.Has("cursor")&&q.Has("review_id"){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  selected:=uuid.Nil;if q.Has("review_id"){selected,e=uuid.Parse(q.Get("review_id"));if e!=nil||selected==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}}
  if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  out,e:=s.Reviews.CollectionCullingGroups(r.Context(),who,tid,id,selected,q.Get("cursor"));if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,200,out)
 })
}

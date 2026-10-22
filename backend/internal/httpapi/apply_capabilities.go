package httpapi
import("net/http";"net/url";"github.com/go-chi/chi/v5";"github.com/google/uuid";"photoeditor/backend/internal/domain";runservice "photoeditor/backend/internal/run")
func(s *Server) collectionApplyCapabilityRoutes(router chi.Router,runs runservice.Service){
 router.Get("/api/v1/tenants/{t}/collections/{id}/apply-capabilities",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","private, no-store")
  who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};collection,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
  query,e:=url.ParseQuery(r.URL.RawQuery);if e!=nil||len(query)!=2||len(query["style_version_id"])!=1||len(query["source_revision"])!=1{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  version,e:=uuid.Parse(query.Get("style_version_id"));if e!=nil||version==uuid.Nil||version.String()!=query.Get("style_version_id")||query.Get("source_revision")==""{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  out,e:=runs.CollectionApplyCapabilities(r.Context(),who,tid,collection,version,query.Get("source_revision"));if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,200,out)
 })
}

package httpapi
import("net/http";"net/url";"github.com/go-chi/chi/v5";"photoeditor/backend/internal/domain";styleservice "photoeditor/backend/internal/style")
func(s *Server) styleAnalysisCapabilityRoutes(router chi.Router,styles styleservice.Service){
 router.Get("/api/v1/tenants/{t}/styles/{id}/analysis-capabilities",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","private, no-store")
  who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};id,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
  q,e:=url.ParseQuery(r.URL.RawQuery);if e!=nil||len(q)!=1||len(q["geometry_enabled"])!=1||(q.Get("geometry_enabled")!="true"&&q.Get("geometry_enabled")!="false"){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  out,e:=styles.AnalysisCapabilities(r.Context(),who,tid,id,q.Get("geometry_enabled")=="true");if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,200,out)
 })
}

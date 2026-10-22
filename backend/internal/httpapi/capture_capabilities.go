package httpapi
import("net/http";"net/url";"github.com/go-chi/chi/v5";"photoeditor/backend/internal/domain";"photoeditor/backend/internal/v2sessions")
func(s *Server) captureCapabilityRoutes(router chi.Router,sessions v2sessions.Service){
 router.Get("/api/v1/tenants/{t}/shoot-sessions/{id}/capture-capabilities",func(w http.ResponseWriter,r *http.Request){w.Header().Set("Cache-Control","private, no-store");who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};id,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return};q,e:=url.ParseQuery(r.URL.RawQuery);if e!=nil||len(q)!=1||len(q["source_revision"])!=1{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return};out,e:=sessions.CaptureCapabilities(r.Context(),who,tid,id,q.Get("source_revision"));if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,200,out)})
}

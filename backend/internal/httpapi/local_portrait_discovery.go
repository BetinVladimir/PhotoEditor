package httpapi
import("net/http";"photoeditor/backend/internal/domain")
func(s *Server)serveLocalPortraitDiscovery(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tenant,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
 run,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
 if s.Reviews==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=s.Reviews.DiscoverLocalPortraitResult(r.Context(),who,tenant,run);if err!=nil{writeError(w,r,err);return};if r.Context().Err()==nil{writeJSON(w,http.StatusOK,out)}
}

package httpapi
import (
 "context"
 "net/http"
 "io"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/semanticreview"
)
type SemanticAppliedPreview interface {ReadAppliedPreview(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(semanticreview.ShownAppliedPreview,error)}
var _ SemanticAppliedPreview=(*semanticreview.ShownAppliedPreviewService)(nil)
func(s *Server)semanticAppliedPreviewRoutes(router chi.Router){
 router.Get("/api/v1/tenants/{t}/semantic-targets/{target}/preview",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  target,err:=routeID(r,"target");if err!=nil{writeError(w,r,err);return}
  body,bodyErr:=io.ReadAll(io.LimitReader(r.Body,1))
  if bodyErr!=nil||len(body)!=0||r.URL.RawQuery!=""{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  w.Header().Set("Cache-Control","private, no-store")
  if s.SemanticAppliedPreview==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  result,err:=s.SemanticAppliedPreview.ReadAppliedPreview(r.Context(),who,tenant,target)
  if r.Context().Err()!=nil{return};if err!=nil{writeError(w,r,semanticReviewError(err));return}
  if result.SchemaVersion!=1||result.TenantID!=tenant||result.TargetID!=target||result.ChildRunID==uuid.Nil||result.WorkingImage.ArtifactID==uuid.Nil||result.After.URL==""{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  writeJSON(w,200,result)
 })
}

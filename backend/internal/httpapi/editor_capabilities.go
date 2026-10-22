package httpapi
import("net/http";"net/url";"github.com/go-chi/chi/v5";"github.com/google/uuid";"photoeditor/backend/internal/domain";runservice "photoeditor/backend/internal/run";"photoeditor/backend/internal/v2draft";"photoeditor/backend/internal/v2render")
func(s *Server) editorCapabilityRoutes(router chi.Router,runs runservice.Service,drafts v2draft.Service){
 router.Get("/api/v1/tenants/{t}/assets/{id}/editor-capabilities",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","private, no-store")
  who,tid,e:=s.tenantRequest(r,false);if e!=nil{writeError(w,r,e);return};asset,e:=routeID(r,"id");if e!=nil{writeError(w,r,e);return}
  q,e:=url.ParseQuery(r.URL.RawQuery);if e!=nil||len(q)<2||len(q)>3||len(q["document_id"])!=1||len(q["expected_revision"])!=1{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return};for key,values:=range q{if len(values)!=1||(key!="document_id"&&key!="expected_revision"&&key!="assume_profile"){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}}
  document,e:=uuid.Parse(q.Get("document_id"));if e!=nil||document==uuid.Nil||document.String()!=q.Get("document_id"){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  var assume *string;if value,ok:=q["assume_profile"];ok{assume=&value[0]}
  out,e:=(runservice.EditorCapabilitiesService{Runs:runs,Drafts:drafts,Qualification:v2render.SQLRenderQualificationResolver{API:s.DB},Calibrations:s.RenderCalibrations}).Read(r.Context(),who,tid,asset,v2render.RenderFreezeRequest{DocumentID:document,ExpectedRevision:q.Get("expected_revision"),AssumeProfile:assume});if e!=nil{writeError(w,r,e);return};if r.Context().Err()!=nil{return};writeJSON(w,200,out)
 })
}

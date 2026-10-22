package httpapi

import (
 "context"
 "encoding/json"
 "net/http"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2draft"
)

type GlareSourceService interface {
 SourceGlareAvailable(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(bool,error)
 PrepareSourceProjectKeyed(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,v2draft.GlareSourcePreparationRequest)(v2draft.GlareSourceClientIntent,error)
 SubmitSourceProjectKeyed(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,v2draft.GlareSourceClientIntent)(v2draft.RestorationAdmissionTicket,error)
}
func(s *Server)glareSourceRoutes(router chi.Router){
 router.Get("/api/v1/tenants/{t}/projects/{id}/glare-generative-capability",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store");who,tenant,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return};project,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||project==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  service,ok:=s.GlareDonorSubmit.(GlareSourceService);if !ok||service==nil{writeJSON(w,http.StatusOK,map[string]bool{"available":false});return};available,err:=service.SourceGlareAvailable(r.Context(),who,tenant,project);if err!=nil{writeError(w,r,semanticProposalError(err));return};if r.Context().Err()!=nil{return};writeJSON(w,http.StatusOK,map[string]bool{"available":available})
 })
 for _,prepare:=range []bool{true,false}{
  prepare:=prepare;path:="/api/v1/tenants/{t}/projects/{id}/glare-generative-submit";if prepare{path="/api/v1/tenants/{t}/projects/{id}/glare-generative-prepare"}
  router.Post(path,func(w http.ResponseWriter,r *http.Request){
   w.Header().Set("Cache-Control","no-store")
   who,tenant,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
   project,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||project==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
   key:=r.Header.Get("Idempotency-Key");if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
   service,ok:=s.GlareDonorSubmit.(GlareSourceService);if !ok||service==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
   r.Body=http.MaxBytesReader(w,r.Body,int64(v2draft.MaxGlareDonorIntentBytes));var raw json.RawMessage
   if err=decodeJSON(r,&raw);err!=nil{writeError(w,r,err);return}
   if prepare{
    request,e:=v2draft.DecodeGlareSourcePreparationRequest(raw);if e!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
    intent,e:=service.PrepareSourceProjectKeyed(r.Context(),who,tenant,project,key,request);if e!=nil{writeError(w,r,semanticProposalError(e));return}
    if e=r.Context().Err();e!=nil{return};writeJSON(w,http.StatusOK,intent);return
   }
   request,e:=v2draft.DecodeGlareSourceClientIntent(raw);if e!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
   ticket,e:=service.SubmitSourceProjectKeyed(r.Context(),who,tenant,project,key,request);if e!=nil{writeError(w,r,semanticProposalError(e));return}
   if e=r.Context().Err();e!=nil{return};if ticket.RunID==uuid.Nil||ticket.WorkflowID==uuid.Nil||ticket.TaskID==uuid.Nil||ticket.State!="queued"{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
   writeJSON(w,http.StatusAccepted,ticket)
  })
 }
}

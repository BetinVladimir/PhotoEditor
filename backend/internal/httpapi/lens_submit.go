package httpapi

import (
 "context"
 "encoding/json"
 "net/http"
 "strconv"
 "photoeditor/backend/internal/v2render"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

type LensSubmit interface { SubmitProjectIntent(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,v2input.LensIntent)(v2input.JobTicket,error) }

type LensProfileCatalog interface {
 ListProfiles(context.Context,auth.Identity,uuid.UUID,uuid.UUID,*uuid.UUID)(v2input.LensCatalogPage,error)
 RegisterProfile(context.Context,auth.Identity,uuid.UUID,uuid.UUID,v2input.Ref)(v2input.LensCatalogEntry,error)
}
func(s *Server)lensRoutes(router chi.Router){
 router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/lens-proposals/{task}/preview",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil{writeError(w,r,err);return}
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));version,e2:=uuid.Parse(chi.URLParam(r,"version"));task,e3:=uuid.Parse(chi.URLParam(r,"task"))
  if e1!=nil || e2!=nil || e3!=nil || project==uuid.Nil || version==uuid.Nil || task==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.LensProposals==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  variant:=r.URL.Query().Get("variant");if variant==""{variant="preview1600"}
  preview,err:=s.LensProposals.Preview(r.Context(),who,tenant,project,version,task,variant);if err!=nil{writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,preview)
 })
 router.Get("/api/v1/tenants/{t}/projects/{id}/lens-intents/{run}",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil {writeError(w,r,err);return}
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));run,e2:=uuid.Parse(chi.URLParam(r,"run"));after:=-1
  if raw:=r.URL.Query().Get("after");raw!="" {after,err=strconv.Atoi(raw)}
  if e1!=nil || e2!=nil || err!=nil || project==uuid.Nil || run==uuid.Nil || after< -1 || after>4999 {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.LensProposals==nil {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  page,err:=s.LensProposals.Status(r.Context(),who,tenant,project,run,after);if err!=nil {writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,page)
 })
 router.Post("/api/v1/tenants/{t}/projects/{id}/versions/{version}/lens-proposals/{task}/apply",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true);if err!=nil {writeError(w,r,err);return}
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));version,e2:=uuid.Parse(chi.URLParam(r,"version"));task,e3:=uuid.Parse(chi.URLParam(r,"task"))
  if e1!=nil || e2!=nil || e3!=nil || project==uuid.Nil || version==uuid.Nil || task==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.LensProposals==nil {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  r.Body=http.MaxBytesReader(w,r.Body,65536);var raw json.RawMessage
  if err=decodeJSON(r,&raw);err!=nil{writeError(w,r,err);return}
  decision,err:=v2render.DecodeLensApplyDecision(raw);if err!=nil{writeError(w,r,semanticProposalError(err));return}
  saved,err:=s.LensProposals.Apply(r.Context(),who,tenant,project,version,task,decision);if err!=nil{writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,saved)
 })
 router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/lens-proposals/{task}",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil {writeError(w,r,err);return}
  project,e1:=uuid.Parse(chi.URLParam(r,"id"));version,e2:=uuid.Parse(chi.URLParam(r,"version"));task,e3:=uuid.Parse(chi.URLParam(r,"task"))
  if e1!=nil || e2!=nil || e3!=nil || project==uuid.Nil || version==uuid.Nil || task==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.LensProposals==nil {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  proposal,err:=s.LensProposals.Describe(r.Context(),who,tenant,project,version,task);if err!=nil {writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,proposal)
 })
 router.Get("/api/v1/tenants/{t}/projects/{id}/lens-profiles",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,false);if err!=nil {writeError(w,r,err);return}
  project,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil || project==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  var after *uuid.UUID
  if raw:=r.URL.Query().Get("after");raw!="" {id,e:=uuid.Parse(raw);if e!=nil || id==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return};after=&id}
  catalog,ok:=s.LensSubmit.(LensProfileCatalog);if !ok {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  page,err:=catalog.ListProfiles(r.Context(),who,tenant,project,after);if err!=nil {writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,page)
 })
 router.Post("/api/v1/tenants/{t}/projects/{id}/lens-profiles",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true);if err!=nil {writeError(w,r,err);return}
  project,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil || project==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  r.Body=http.MaxBytesReader(w,r.Body,1024);var ref v2input.Ref
  if err=decodeJSON(r,&ref);err!=nil {writeError(w,r,err);return}
  catalog,ok:=s.LensSubmit.(LensProfileCatalog);if !ok {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  entry,err:=catalog.RegisterProfile(r.Context(),who,tenant,project,ref);if err!=nil {writeError(w,r,semanticProposalError(err));return}
  if r.Context().Err()!=nil{return};w.Header().Set("Cache-Control","no-store");writeJSON(w,200,entry)
 })
 router.Post("/api/v1/tenants/{t}/projects/{id}/lens-intents",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true);if err!=nil {writeError(w,r,err);return}
  project,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil || project==uuid.Nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  key:=r.Header.Get("Idempotency-Key");if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("IDEMPOTENCY_KEY_REQUIRED",400));return}
  r.Body=http.MaxBytesReader(w,r.Body,1048576)
  var raw json.RawMessage
  if err=decodeJSON(r,&raw);err!=nil {writeError(w,r,err);return}
  intent,err:=v2input.DecodeLensIntent(raw);if err!=nil {writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.LensSubmit==nil {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  ticket,err:=s.LensSubmit.SubmitProjectIntent(r.Context(),who,tenant,project,key,intent)
  if err!=nil {writeError(w,r,semanticProposalError(err));return}
  run,runErr:=v2input.RunID(tenant,who.UserID,key);workflow,workflowErr:=v2input.WorkflowID(run)
  if runErr!=nil || workflowErr!=nil || ticket.RunID!=run || ticket.WorkflowID!=workflow || ticket.State!="queued" {writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  if r.Context().Err()!=nil {return}
  w.Header().Set("Cache-Control","no-store")
  writeJSON(w,202,semanticProposeTicket{RunID:ticket.RunID,WorkflowID:ticket.WorkflowID,State:ticket.State})
 })
}

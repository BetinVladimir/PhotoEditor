package httpapi

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2region"
)

// Separate from scalar RegionQueue: this path always requires a registered
// current authored frozen draft. It returns a compound request, not an Apply run.
type AuthoredRegionQueue interface {
 Queue(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,v2region.AuthoredRegionRequest)(v2region.AuthoredRegionTicket,error)
 Status(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(v2region.AuthoredRegionTicket,error)
 Cancel(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID)(v2region.AuthoredRegionTicket,error)
}
func authoredRegionHTTPError(err error)error{
 switch {
 case errors.Is(err,v2input.ErrConflict):return domain.NewError("IDEMPOTENCY_CONFLICT",409)
 case errors.Is(err,v2input.ErrExpired):return domain.NewError("AUTHORED_REGION_STALE",410)
 case errors.Is(err,v2region.ErrRegionInvalid):return domain.NewError("VALIDATION_FAILED",422)
 case errors.Is(err,v2region.ErrRegionUnavailable):return domain.NewError("DEPENDENCY_UNAVAILABLE",503)
 default:return err
 }
}
func(s *Server)authoredRegionRoutes(r chi.Router){
 const base="/api/v1/tenants/{t}/assets/{id}/authored-region-previews"
 r.Post(base,func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
  asset,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
  key:=r.Header.Get("Idempotency-Key");if !idempotency.ValidKey(key){writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  r.Body=http.MaxBytesReader(w,r.Body,16384);var raw json.RawMessage
  if err=decodeJSON(r,&raw);err!=nil{writeError(w,r,err);return}
  request,err:=v2region.DecodeAuthoredRegionRequest(raw);if err!=nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
  if s.AuthoredRegionQueue==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  ticket,err:=s.AuthoredRegionQueue.Queue(r.Context(),who,tenant,asset,key,request)
  if err!=nil{writeError(w,r,authoredRegionHTTPError(err));return}
  if !validAuthoredRegionTicket(ticket){writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
  writeJSON(w,202,ticket)
 })
 for _,cancel:=range []bool{false,true}{
  cancel:=cancel
  handler:=func(w http.ResponseWriter,r *http.Request){
   who,tenant,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
   asset,err:=routeID(r,"id");if err!=nil{writeError(w,r,err);return}
   request,err:=routeID(r,"request");if err!=nil{writeError(w,r,err);return}
   if s.AuthoredRegionQueue==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
   var ticket v2region.AuthoredRegionTicket
   if cancel{ticket,err=s.AuthoredRegionQueue.Cancel(r.Context(),who,tenant,asset,request)}else{ticket,err=s.AuthoredRegionQueue.Status(r.Context(),who,tenant,asset,request)}
   if err!=nil{writeError(w,r,authoredRegionHTTPError(err));return}
   if !validAuthoredRegionTicket(ticket)||ticket.ID!=request{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
   writeJSON(w,200,ticket)
  }
  if cancel{r.Post(base+"/{request}/cancel",handler)}else{r.Get(base+"/{request}",handler)}
 }
}
func validAuthoredRegionTicket(t v2region.AuthoredRegionTicket)bool{
 if t.ID==uuid.Nil||t.Revision<1||len(t.IntentSHA)!=64{return false}
 for _,c:=range t.IntentSHA{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}}
 switch t.Phase{
 case v2region.AuthoredRegionAdmitted,v2region.AuthoredRegionRendering,v2region.AuthoredRegionCropping,v2region.AuthoredRegionReady,
 v2region.AuthoredRegionCancelling,v2region.AuthoredRegionCancelled,v2region.AuthoredRegionStale,v2region.AuthoredRegionFailed:return true
 default:return false
 }
}

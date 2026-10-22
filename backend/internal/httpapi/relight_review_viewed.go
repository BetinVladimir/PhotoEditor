package httpapi

import (
 "context"
 "net/http"
 "time"
 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
)

type RelightViewedProvider interface {
 AcknowledgeRelightViewed(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string,[]byte)(review.RelightViewedReceipt,error)
}
func (s *Server) serveRelightReviewViewed(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","private, no-store")
 who,tid,err:=s.tenantRequest(r,true);if err!=nil{writeError(w,r,err);return}
 id,err:=uuid.Parse(chi.URLParam(r,"id"));if err!=nil||id==uuid.Nil{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 var request struct{Before string `json:"before_token"`;After string `json:"after_token"`}
 if err:=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 if request.Before==""||request.After==""||len(request.Before)>1600||len(request.After)>1600{writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return}
 provider,ok:=s.ReviewPreviews.(RelightViewedProvider);if !ok{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=provider.AcknowledgeRelightViewed(r.Context(),who,tid,id,request.Before,request.After,s.SelectedPreviewKey);if err!=nil{writeError(w,r,err);return}
 grant,grantErr:=review.OpenRelightViewedGrant(s.SelectedPreviewKey,out.Token,time.Now())
 before,beforeErr:=review.OpenPendingPreviewGrant(s.SelectedPreviewKey,request.Before,time.Now())
 after,afterErr:=review.OpenPendingPreviewGrant(s.SelectedPreviewKey,request.After,time.Now())
 if out.Version!=1||grantErr!=nil||beforeErr!=nil||afterErr!=nil||grant.Before!=before||grant.After!=after||grant.Before.Tenant!=tid||grant.Before.Review!=id||grant.Before.User!=who.UserID||grant.Before.Session!=who.SessionID||!out.ExpiresAt.Equal(time.Unix(grant.ExpiresUnix,0)){
  writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return
 }
 if err:=r.Context().Err();err!=nil{writeError(w,r,err);return}
 writeJSON(w,http.StatusOK,out)
}

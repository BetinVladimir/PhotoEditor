package httpapi

import (
 "context"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/review"
)
type relightViewedHTTPStub struct{calls int}
func (s *relightViewedHTTPStub) Preview(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string)(review.CandidatePreview,error){return review.CandidatePreview{},nil}
func (s *relightViewedHTTPStub) AcknowledgeRelightViewed(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,string,[]byte)(review.RelightViewedReceipt,error){s.calls++;return review.RelightViewedReceipt{},nil}
func TestRelightViewedRequiresAuthenticatedSession(t *testing.T){
 provider:=&relightViewedHTTPStub{}
 handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("relight-viewed")},ReviewPreviews:provider}).Routes()
 response:=httptest.NewRecorder()
 request:=httptest.NewRequest("POST","/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/relight-viewed",strings.NewReader(`{"before_token":"a","after_token":"b"}`))
 request.Header.Set("Content-Type","application/json")
 handler.ServeHTTP(response,request)
 if response.Code!=401||provider.calls!=0{t.Fatal("unauthenticated acknowledgement reached provider",response.Code,provider.calls)}
}

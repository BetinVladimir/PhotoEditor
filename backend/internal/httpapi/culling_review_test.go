package httpapi

import (
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestCullingReviewEndpointsRequireAuthenticatedSession(t *testing.T){
 handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("culling-review")}}).Routes()
 for _,endpoint:=range []struct{method,path,body string}{{"GET","culling-items",""},{"POST","culling-decision",`{"expected_revision":1,"candidate_hash":"`+strings.Repeat("a",64)+`","selected_item_ids":[],"confirmed_eyes_item_ids":[],"never_delete":true}`}}{
  request:=httptest.NewRequest(endpoint.method,"/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/"+endpoint.path,strings.NewReader(endpoint.body));request.Header.Set("Content-Type","application/json");request.Header.Set("Idempotency-Key",uuid.NewString());response:=httptest.NewRecorder();handler.ServeHTTP(response,request);if response.Code!=401{t.Fatal("unauthenticated culling access",endpoint.path,response.Code)}
 }
}

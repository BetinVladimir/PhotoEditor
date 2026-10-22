package httpapi

import (
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestOpenEyesApplyRequiresAuthenticatedSession(t *testing.T){
 handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("open-eyes-apply")}}).Routes()
 request:=httptest.NewRequest("POST","/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/open-eyes-apply",strings.NewReader(`{"decision_hash":"`+strings.Repeat("a",64)+`"}`))
 request.Header.Set("Content-Type","application/json")
 response:=httptest.NewRecorder();handler.ServeHTTP(response,request)
 if response.Code!=401{t.Fatal("unauthenticated apply allowed",response.Code)}
}

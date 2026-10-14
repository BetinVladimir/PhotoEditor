package httpapi

import (
 "net/http/httptest"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestRenderSelectedPreviewRequiresAuthentication(t *testing.T){
 server:=&Server{Auth:&auth.Service{Pepper:[]byte("render-selected-preview")}}
 url:="/api/v1/tenants/"+uuid.NewString()+"/reviews/"+uuid.NewString()+"/selected-preview/preview1600"
 response:=httptest.NewRecorder()
 server.Routes().ServeHTTP(response,httptest.NewRequest("GET",url,nil))
 if response.Code!=401||response.Header().Get("Cache-Control")!="private, no-store"{
  t.Fatal("selected preview exposed without session",response.Code,response.Header())
 }
}

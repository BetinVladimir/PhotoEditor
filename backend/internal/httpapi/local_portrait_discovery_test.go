package httpapi
import("net/http/httptest";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestLocalPortraitDiscoveryRequiresAuthentication(t *testing.T){
 handler:=(&Server{Auth:&auth.Service{Pepper:[]byte("local-portrait-discovery")}}).Routes()
 request:=httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+"/runs/"+uuid.NewString()+"/local-portrait-result",nil)
 recorder:=httptest.NewRecorder();handler.ServeHTTP(recorder,request)
 if recorder.Code!=401{t.Fatalf("unauthenticated discovery status %d",recorder.Code)}
 if recorder.Header().Get("Cache-Control")!="private, no-store"{t.Fatal("discovery response cacheable")}
}

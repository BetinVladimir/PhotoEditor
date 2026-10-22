package httpapi
import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "photoeditor/backend/internal/config"
)
func TestGenerationReplayOnlyHeaderAllowedForConfiguredAppOrigin(t *testing.T){
 server:=Server{Config:config.Config{AppOrigin:"https://editor.example"}}
 handler:=server.cors(http.HandlerFunc(func(http.ResponseWriter,*http.Request){t.Fatal("preflight reached mutation")}))
 request:=httptest.NewRequest(http.MethodOptions,"/api/v1/tenants/t/generations",nil)
 request.Header.Set("Origin","https://editor.example");request.Header.Set("Access-Control-Request-Method","POST");request.Header.Set("Access-Control-Request-Headers","Generation-Replay-Only,Idempotency-Key")
 response:=httptest.NewRecorder();handler.ServeHTTP(response,request)
 if response.Code!=204 || !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"),"Generation-Replay-Only") {t.Fatal("replay-only preflight refused",response.Code,response.Header())}
}

func TestRelightReplayOnlyHeaderAllowedForConfiguredAppOrigin(t *testing.T){
 server:=Server{Config:config.Config{AppOrigin:"https://editor.example"}}
 handler:=server.cors(http.HandlerFunc(func(http.ResponseWriter,*http.Request){t.Fatal("preflight reached admission")}))
 request:=httptest.NewRequest(http.MethodOptions,"/api/v1/tenants/t/projects/p/relight-intents",nil)
 request.Header.Set("Origin","https://editor.example")
 request.Header.Set("Access-Control-Request-Method","POST")
 request.Header.Set("Access-Control-Request-Headers","Relight-Replay-Only,Idempotency-Key")
 response:=httptest.NewRecorder();handler.ServeHTTP(response,request)
 if response.Code!=204 || !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"),"Relight-Replay-Only") {t.Fatal("relight replay preflight refused",response.Code,response.Header())}
}

func TestSaveEditsResolutionHeaderAllowedOnlyForConfiguredOrigin(t *testing.T){
 server:=Server{Config:config.Config{AppOrigin:"https://editor.example"}}
 handler:=server.cors(http.HandlerFunc(func(http.ResponseWriter,*http.Request){t.Fatal("preflight reached save-edits")}))
 for _,origin:=range []string{"https://editor.example","https://foreign.example"}{
  request:=httptest.NewRequest(http.MethodOptions,"/api/v1/tenants/t/collections/c/save-edits",nil)
  request.Header.Set("Origin",origin)
  request.Header.Set("Access-Control-Request-Method","POST")
  request.Header.Set("Access-Control-Request-Headers","Save-Edits-Resolve-Only,Idempotency-Key,X-CSRF-Token")
  response:=httptest.NewRecorder();handler.ServeHTTP(response,request)
  if origin=="https://editor.example"{
   if response.Code!=204||!strings.Contains(response.Header().Get("Access-Control-Allow-Headers"),"Save-Edits-Resolve-Only"){t.Fatal("saved command cannot be resolved from the browser",response.Code,response.Header())}
  }else if response.Code!=403||response.Header().Get("Access-Control-Allow-Origin")!=""{t.Fatal("foreign origin gained resolution access",response.Code,response.Header())}
 }
}

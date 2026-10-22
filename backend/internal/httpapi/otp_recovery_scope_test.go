package httpapi

import (
 "encoding/base64"
 "net/http/httptest"
 "testing"
)
func TestOTPRecoveryScopeKeepsLegacyAndBindsNativeSecret(t *testing.T){
 s:=&Server{}
 legacy:=httptest.NewRequest("POST","/api/v1/auth/otp/verify",nil)
 if scope,err:=s.otpRecoveryScope(legacy);err!=nil||scope!=nil{t.Fatal("legacy request changed",err)}
 nonce:=base64.RawURLEncoding.EncodeToString(make([]byte,32))
 native:=httptest.NewRequest("POST","/api/v1/auth/otp/verify",nil)
 native.Header.Set("X-Photo-Client","native");native.Header.Set("Idempotency-Key","stable-key")
 if _,err:=s.otpRecoveryScope(native);err==nil{t.Fatal("native recovery without independent nonce")}
 native.Header.Set("X-Photo-Auth-Nonce",nonce)
 scope,err:=s.otpRecoveryScope(native)
 if err!=nil||scope==nil||scope.Key!="stable-key"||scope.Bootstrap!=nonce{t.Fatal("native scope lost",err)}
 native.Header.Add("X-Photo-Auth-Nonce",nonce)
 if _,err:=s.otpRecoveryScope(native);err==nil{t.Fatal("ambiguous nonce accepted")}
}
func TestOTPRecoveryScopeRejectsAmbiguousKeysAndMalformedNativeNonce(t *testing.T){
 s:=&Server{}
 for _,nonce:=range []string{"short",base64.RawURLEncoding.EncodeToString(make([]byte,31)),base64.StdEncoding.EncodeToString(make([]byte,32))}{
  r:=httptest.NewRequest("POST","/api/v1/auth/otp/request",nil)
  r.Header.Set("X-Photo-Client","native");r.Header.Set("Idempotency-Key","key");r.Header.Set("X-Photo-Auth-Nonce",nonce)
  if _,err:=s.otpRecoveryScope(r);err==nil{t.Fatal("malformed nonce accepted")}
 }
 web:=httptest.NewRequest("POST","/api/v1/auth/otp/request",nil)
 web.Header.Set("Idempotency-Key","key");web.Header.Set("X-CSRF-Token","already-validated-web-bootstrap")
 scope,err:=s.otpRecoveryScope(web)
 if err!=nil||scope==nil||scope.Bootstrap!=web.Header.Get("X-CSRF-Token"){t.Fatal("web bootstrap lost",err)}
 web.Header.Add("Idempotency-Key","another")
 if _,err:=s.otpRecoveryScope(web);err==nil{t.Fatal("duplicate key accepted")}
}

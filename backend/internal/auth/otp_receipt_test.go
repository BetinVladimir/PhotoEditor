package auth

import (
 "bytes"
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
)

func TestOTPReceiptEncryptionAndCallerBinding(t *testing.T) {
 s:=&Service{Pepper:bytes.Repeat([]byte{7},32)}
 id:=uuid.New();r:=OTPReceiptRequest{Key:"same-key",Bootstrap:"0123456789012345678901234567890123456789012"}
 binding:=s.otpReceiptBinding(id,"123456","web",r)
 tokens:=Tokens{User:User{ID:uuid.New()},SessionID:uuid.New(),Access:"private-access",Refresh:"private-refresh",CSRF:"private-csrf",RefreshExpires:time.Now().UTC().Add(time.Hour)}
 sealed,err:=s.sealOTPReceipt(tokens,binding);if err!=nil{t.Fatal(err)}
 for _,secret:=range []string{tokens.Access,tokens.Refresh,tokens.CSRF,r.Bootstrap,"123456"} { if bytes.Contains(sealed,[]byte(secret)){t.Fatal("plaintext secret persisted")} }
 plain,err:=s.otpReceiptKey().Open("otp-verify-receipt-v1",sealed,binding);if err!=nil||!bytes.Contains(plain,[]byte("refresh_expires")){t.Fatal("receipt lost refresh cookie expiry",err)}
 changed:=[]struct{id uuid.UUID;code,client string;r OTPReceiptRequest}{
  {uuid.New(),"123456","web",r},{id,"654321","web",r},{id,"123456","native",r},
  {id,"123456","web",OTPReceiptRequest{Key:"other-key",Bootstrap:r.Bootstrap}},
  {id,"123456","web",OTPReceiptRequest{Key:r.Key,Bootstrap:r.Bootstrap+"x"}},
 }
 for _,other:=range changed { if _,err=s.otpReceiptKey().Open("otp-verify-receipt-v1",sealed,s.otpReceiptBinding(other.id,other.code,other.client,other.r));err==nil {t.Fatal("different caller decrypted receipt")} }
 sealed[len(sealed)-1]^=1
 if _,err=s.otpReceiptKey().Open("otp-verify-receipt-v1",sealed,binding);err==nil{t.Fatal("modified ciphertext accepted")}
}
func TestOTPReceiptRejectsInvalidScopeBeforeDatabase(t *testing.T) {
 s:=&Service{}
 for _,r:=range []OTPReceiptRequest{{Key:"",Bootstrap:"long-enough-bootstrap-credential-123"},{Key:"key",Bootstrap:"short"}} {
  if _,err:=s.VerifyOTPWithReceipt(context.Background(),uuid.New(),"123456","web",r);err==nil{t.Fatal("invalid scope accepted")}
 }
}

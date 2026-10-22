package auth

import (
 "context"
 "errors"
 "encoding/json"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgconn"
)

func TestIntegrationOTPVerifyRecoversExactSessionAndRefusesOtherScopes(t *testing.T){
 s:=testService(t);ctx:=context.Background()
 challenge,code:=requestCode(t,s,uuid.NewString()+"@otp-recovery.test")
 receipt:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 first,err:=s.VerifyOTPWithReceipt(ctx,challenge.ID,code,"native",receipt);if err!=nil{t.Fatal(err)}
 replay,err:=s.VerifyOTPWithReceipt(ctx,challenge.ID,code,"native",receipt);if err!=nil{t.Fatal(err)}
 a,_:=json.Marshal(first);b,_:=json.Marshal(replay)
 if string(a)!=string(b)||!first.RefreshExpires.Equal(replay.RefreshExpires){t.Fatal("lost-response retry changed session/cookies")}
 for _,wrong:=range []OTPReceiptRequest{{Key:uuid.NewString(),Bootstrap:receipt.Bootstrap},{Key:receipt.Key,Bootstrap:receipt.Bootstrap+"x"}}{
  if _,err=s.VerifyOTPWithReceipt(ctx,challenge.ID,code,"native",wrong);err==nil{t.Fatal("foreign caller received credentials")}
 }
 if _,err=s.VerifyOTPWithReceipt(ctx,challenge.ID,code,"web",receipt);err==nil{t.Fatal("cross-client replay")}
 if _,err=s.VerifyOTP(ctx,challenge.ID,code,"native");err==nil{t.Fatal("legacy consumed OTP reused")}
 var count int
 if err=s.DB.QueryRow(ctx,"SELECT count(*) FROM photo.sessions WHERE user_id=$1",first.User.ID).Scan(&count);err!=nil||count!=1{t.Fatal("replay minted session",count,err)}
 if _,err=s.DB.Exec(ctx,"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",first.SessionID);err!=nil{t.Fatal(err)}
 if _,err=s.VerifyOTPWithReceipt(ctx,challenge.ID,code,"native",receipt);err==nil{t.Fatal("revoked session replayed")}
}
func TestIntegrationOTPRequestRetryDoesNotSendMailOrIncreaseRateCounters(t *testing.T){
 s:=testService(t);ctx:=context.Background();email:=uuid.NewString()+"@otp-request-recovery.test"
 receipt:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 first,err:=s.RequestOTPWithReceipt(ctx,email,"en",uuid.NewString(),"native",receipt);if err!=nil{t.Fatal(err)}
 replay,err:=s.RequestOTPWithReceipt(ctx,email,"en",uuid.NewString(),"native",receipt);if err!=nil||replay!=first{t.Fatal("request receipt changed",replay,err)}
 var count int
 if err=s.DB.QueryRow(ctx,"SELECT count(*) FROM photo.mail_payloads WHERE challenge_id=$1",first.ID).Scan(&count);err!=nil||count!=1{t.Fatal("duplicate mail",count,err)}
 if err=s.DB.QueryRow(ctx,"SELECT count FROM photo.auth_rate_limits WHERE scope_hash=$1 AND window_start=$2",MAC(s.Pepper,"otp-rate:email",email),s.now().Truncate(time.Hour)).Scan(&count);err!=nil||count!=1{t.Fatal("replay consumed rate quota",count,err)}
 if _,err=s.RequestOTPWithReceipt(ctx,email,"ru",uuid.NewString(),"native",receipt);err==nil{t.Fatal("changed request reused key")}
}

func TestIntegrationOTPReceiptExpiryAndEpochRefuseReplay(t *testing.T){
 for _,mode:=range []string{"expiry","epoch","refresh-used","disabled"}{
  t.Run(mode,func(t *testing.T){
   s:=testService(t);ctx:=context.Background();c,code:=requestCode(t,s,uuid.NewString()+"@otp-expiry.test")
   r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
   out,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);if err!=nil{t.Fatal(err)}
   switch mode{
   case "expiry":_,err=s.DB.Exec(ctx,"UPDATE photo.auth_challenges SET verify_receipt_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1",c.ID)
   case "epoch":_,err=s.DB.Exec(ctx,"UPDATE photo.users SET auth_epoch=auth_epoch+1 WHERE id=$1",out.User.ID)
   case "refresh-used":_,err=s.DB.Exec(ctx,"UPDATE photo.sessions SET used_at=clock_timestamp() WHERE id=$1",out.SessionID)
   case "disabled":_,err=s.DB.Exec(ctx,"UPDATE photo.users SET status='disabled' WHERE id=$1",out.User.ID)
   }
   if err!=nil{t.Fatal(err)}
   if _,err=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);err==nil{t.Fatal("stale receipt replayed",mode)}
  })
 }
}
func TestIntegrationConcurrentOTPVerifyCreatesOneSession(t *testing.T){
 s:=testService(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 c,code:=requestCode(t,s,uuid.NewString()+"@otp-concurrent.test")
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 type reply struct{tokens Tokens;err error};replies:=make(chan reply,2);start:=make(chan struct{})
 for i:=0;i<2;i++{go func(){<-start;tokens,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);replies<-reply{tokens,err}}()};close(start)
 first,second:=<-replies,<-replies
 if first.err!=nil||second.err!=nil||first.tokens.SessionID!=second.tokens.SessionID||first.tokens.Refresh!=second.tokens.Refresh{t.Fatal("concurrent verify changed session",first.err,second.err)}
}
func TestIntegrationFailedOTPStillCommitsAttemptWithoutReceipt(t *testing.T){
 s:=testService(t);ctx:=context.Background();c,code:=requestCode(t,s,uuid.NewString()+"@otp-attempt.test")
 wrong:="000000";if code==wrong{wrong="111111"}
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 if _,err:=s.VerifyOTPWithReceipt(ctx,c.ID,wrong,"native",r);err==nil{t.Fatal("invalid OTP accepted")}
 var attempts int;var noReceipt bool
 if err:=s.DB.QueryRow(ctx,"SELECT attempts,verify_receipt_cipher IS NULL FROM photo.auth_challenges WHERE id=$1",c.ID).Scan(&attempts,&noReceipt);err!=nil||attempts!=1||!noReceipt{t.Fatal("attempt accounting rolled back",attempts,noReceipt,err)}
 if _,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:r.Bootstrap});err!=nil{t.Fatal("valid corrected code refused",err)}
}

// A stale application clock must not override the final database-clock fence.
func TestIntegrationOTPReplayUsesDatabaseExpiryFence(t *testing.T){
 s:=testService(t);ctx:=context.Background()
 c,code:=requestCode(t,s,uuid.NewString()+"@otp-db-clock.test")
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 if _,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);err!=nil{t.Fatal(err)}
 now:=time.Now().UTC()
 if _,err:=s.DB.Exec(ctx,"UPDATE photo.auth_challenges SET verify_receipt_expires_at=$2 WHERE id=$1",c.ID,now.Add(-time.Second));err!=nil{t.Fatal(err)}
 s.Now=func()time.Time{return now.Add(-time.Minute)}
 if _,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);err==nil{t.Fatal("application clock revived expired receipt")}
}
func TestIntegrationConcurrentOTPRequestSendsOneMail(t *testing.T){
 s:=testService(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 email:=uuid.NewString()+"@otp-request-concurrent.test"
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 type reply struct{out Challenge;err error};results:=make(chan reply,2);start:=make(chan struct{})
 for i:=0;i<2;i++{go func(){<-start;out,err:=s.RequestOTPWithReceipt(ctx,email,"en",uuid.NewString(),"native",r);results<-reply{out,err}}()};close(start)
 a,b:=<-results,<-results
 if a.err!=nil||b.err!=nil||a.out!=b.out{t.Fatal("concurrent request changed challenge",a.err,b.err)}
 var count int
 if err:=s.DB.QueryRow(ctx,"SELECT count(*) FROM photo.mail_payloads WHERE challenge_id=$1",a.out.ID).Scan(&count);err!=nil||count!=1{t.Fatal("concurrent request duplicated mail",count,err)}
}
func TestIntegrationCanceledOTPVerifyLeavesChallengeUsable(t *testing.T){
 s:=testService(t);c,code:=requestCode(t,s,uuid.NewString()+"@otp-cancel.test")
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.VerifyOTPWithReceipt(ctx,c.ID,code,"native",r);err==nil{t.Fatal("canceled verification accepted")}
 var untouched bool
 if err:=s.DB.QueryRow(context.Background(),"SELECT consumed_at IS NULL AND verify_receipt_cipher IS NULL FROM photo.auth_challenges WHERE id=$1",c.ID).Scan(&untouched);err!=nil||!untouched{t.Fatal("cancellation consumed challenge",err)}
 if _,err:=s.VerifyOTPWithReceipt(context.Background(),c.ID,code,"native",r);err!=nil{t.Fatal("valid retry failed",err)}
}

type otpUnavailableEntropy struct{}
func(otpUnavailableEntropy)Read([]byte)(int,error){return 0,errors.New("entropy unavailable")}
func TestIntegrationOTPRequestRecoveryDoesNotAllocateEntropy(t *testing.T){
 s:=testService(t);ctx:=context.Background()
 r:=OTPReceiptRequest{Key:uuid.NewString(),Bootstrap:"0123456789012345678901234567890123456789012"}
 email:=uuid.NewString()+"@otp-entropy.test"
 first,err:=s.RequestOTPWithReceipt(ctx,email,"en",uuid.NewString(),"native",r);if err!=nil{t.Fatal(err)}
 s.Random=otpUnavailableEntropy{}
 again,err:=s.RequestOTPWithReceipt(ctx,email,"en",uuid.NewString(),"native",r)
 if err!=nil||again!=first{t.Fatal("committed receipt depended on new entropy",err)}
}

func TestIntegrationOTPReceiptConstraintRefusesPartialOrUnconsumedRows(t *testing.T){
 s:=testService(t);ctx:=context.Background();c,_:=requestCode(t,s,uuid.NewString()+"@otp-constraint.test")
 if err:=RequireOTPReceiptSchema(ctx,s.DB);err!=nil{t.Fatal("installed role/schema missing",err)}
 for _,tc:=range []struct{name,assignment string}{
  {"binding-only","verify_receipt_binding=decode(repeat('ab',32),'hex')"},
  {"cipher-only","verify_receipt_cipher=decode(repeat('ab',29),'hex')"},
  {"expiry-only","verify_receipt_expires_at=clock_timestamp()+interval '1 minute'"},
  {"epoch-only","verify_receipt_epoch=0"},
  {"unconsumed-complete","verify_receipt_binding=decode(repeat('ab',32),'hex'),verify_receipt_cipher=decode(repeat('ab',29),'hex'),verify_receipt_expires_at=clock_timestamp()+interval '1 minute',verify_receipt_epoch=0"},
  {"null-binding-consumed","consumed_at=clock_timestamp(),verify_receipt_cipher=decode(repeat('ab',29),'hex'),verify_receipt_expires_at=clock_timestamp()+interval '1 minute',verify_receipt_epoch=0"},
  {"invalid-binding-size","consumed_at=clock_timestamp(),verify_receipt_binding=decode(repeat('ab',31),'hex'),verify_receipt_cipher=decode(repeat('ab',29),'hex'),verify_receipt_expires_at=clock_timestamp()+interval '1 minute',verify_receipt_epoch=0"},
  {"negative-epoch","consumed_at=clock_timestamp(),verify_receipt_binding=decode(repeat('ab',32),'hex'),verify_receipt_cipher=decode(repeat('ab',29),'hex'),verify_receipt_expires_at=clock_timestamp()+interval '1 minute',verify_receipt_epoch=-1"},
 }{t.Run(tc.name,func(t *testing.T){
  tx,err:=s.DB.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
  _,err=tx.Exec(ctx,"UPDATE photo.auth_challenges SET "+tc.assignment+" WHERE id=$1",c.ID)
  var pe *pgconn.PgError
  if !errors.As(err,&pe)||pe.Code!="23514"||pe.ConstraintName!="auth_verify_receipt_complete"{t.Fatal("invalid receipt shape admitted or unrelated failure",err)}
 })}
}

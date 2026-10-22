package auth

import (
 "context"
 "encoding/hex"
 "encoding/json"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// requestOTPReceipt is used only within RequestOTP's transaction, before rate
// counters and mail creation. Failed/rate-limited requests never cache success.
type otpRequestReceipt struct { Scope []byte; Key,Hash string }
func (s *Service) RequestOTPWithReceipt(ctx context.Context,raw,locale,ip,client string,r OTPReceiptRequest)(Challenge,error){
 if !idempotency.ValidKey(r.Key)||len(r.Bootstrap)<32||len(r.Bootstrap)>512||(client!="web"&&client!="native"){return Challenge{},domain.NewError("VALIDATION_FAILED",422)}
 email,err:=NormalizeEmail(raw);if err!=nil{return Challenge{},domain.NewError("VALIDATION_FAILED",422)}
 hash,err:=canonical.Hash(struct{Email,Locale,Client string}{email,locale,client});if err!=nil{return Challenge{},err}
 receipt:=otpRequestReceipt{Scope:MAC(s.Pepper,"otp-request-receipt-v1",client,r.Bootstrap),Key:r.Key,Hash:hash}
 return s.requestOTP(ctx,raw,locale,ip,&receipt)
}
func lockOTPRequestReceipt(ctx context.Context,tx pgx.Tx,r otpRequestReceipt)(*Challenge,error){
 if _,err:=tx.Exec(ctx,"SELECT pg_advisory_xact_lock(hashtextextended($1,0))","otp-request-receipt:"+hex.EncodeToString(r.Scope)+":"+r.Key);err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 var hash string;var body []byte;var live bool
 err:=tx.QueryRow(ctx,`SELECT request_hash,response,expires_at>clock_timestamp() FROM photo.auth_idempotency_records WHERE scope_hash=$1 AND key=$2 FOR UPDATE`,r.Scope,r.Key).Scan(&hash,&body,&live)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if !live||hash!=r.Hash{return nil,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 var challenge Challenge
 if json.Unmarshal(body,&challenge)!=nil||challenge.ID.String()=="00000000-0000-0000-0000-000000000000"||challenge.ExpiresIn!=300{return nil,errors.New("invalid OTP challenge receipt")}
 if err:=ctx.Err();err!=nil{return nil,err}
 return &challenge,nil
}
func saveOTPRequestReceipt(ctx context.Context,tx pgx.Tx,r otpRequestReceipt,out Challenge,now time.Time)error{
 body,err:=json.Marshal(out);if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO photo.auth_idempotency_records(scope_hash,key,request_hash,response,expires_at) VALUES($1,$2,$3,$4,$5)`,r.Scope,r.Key,r.Hash,json.RawMessage(body),now.Add(5*time.Minute))
 return err
}

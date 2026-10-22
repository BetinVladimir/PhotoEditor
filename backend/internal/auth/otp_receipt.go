package auth

import (
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// OTPReceiptRequest binds recovery to the same client-held bootstrap credential.
// It is never stored directly, logged, or included in a public response.
type OTPReceiptRequest struct { Key, Bootstrap string }
type otpReceiptBody struct {
 Tokens Tokens `json:"tokens"`
 RefreshExpires time.Time `json:"refresh_expires"`
}
func (s *Service) VerifyOTPWithReceipt(ctx context.Context,id uuid.UUID,code,client string,receipt OTPReceiptRequest)(Tokens,error){
 if !idempotency.ValidKey(receipt.Key)||len(receipt.Bootstrap)<32||len(receipt.Bootstrap)>512 {
  return Tokens{},domain.NewError("VALIDATION_FAILED",422)
 }
 return s.verifyOTP(ctx,id,code,client,&receipt)
}
func (s *Service) otpReceiptKey() KeyRing {
 const keyID="otp-verify-receipt-v1"
 return KeyRing{Active:keyID,Keys:map[string][]byte{keyID:MAC(s.Pepper,"otp-verify-receipt-encryption-v1")}}
}
func (s *Service) otpReceiptBinding(id uuid.UUID,code,client string,r OTPReceiptRequest) []byte {
 return MAC(s.Pepper,"otp-verify-receipt-binding-v1",id.String(),client,r.Key,r.Bootstrap,code)
}
func (s *Service) sealOTPReceipt(tokens Tokens,binding []byte)([]byte,error){
 plain,err:=json.Marshal(otpReceiptBody{Tokens:tokens,RefreshExpires:tokens.RefreshExpires})
 if err!=nil{return nil,err}
 _,cipher,err:=s.otpReceiptKey().Seal(plain,binding)
 return cipher,err
}
func (s *Service) openOTPReceipt(ctx context.Context,tx pgx.Tx,cipher,binding []byte,epoch int64,receiptExpires time.Time)(Tokens,error){
 plain,err:=s.otpReceiptKey().Open("otp-verify-receipt-v1",cipher,binding)
 if err!=nil{return Tokens{},denied()}
 var body otpReceiptBody
 if json.Unmarshal(plain,&body)!=nil||body.Tokens.User.ID==uuid.Nil||body.Tokens.SessionID==uuid.Nil||body.Tokens.Access==""||body.Tokens.Refresh==""||body.Tokens.CSRF==""||body.RefreshExpires.IsZero(){return Tokens{},denied()}
 var currentEpoch int64
 err=tx.QueryRow(ctx,`SELECT auth_epoch FROM photo.users WHERE id=$1 AND status='active' FOR SHARE`,body.Tokens.User.ID).Scan(&currentEpoch)
 if err!=nil {if err==pgx.ErrNoRows{return Tokens{},denied()};return Tokens{},err}
 if currentEpoch!=epoch{return Tokens{},denied()}
 var session uuid.UUID
 err=tx.QueryRow(ctx,`SELECT id FROM photo.sessions
  WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL AND used_at IS NULL
   AND expires_at>clock_timestamp() AND expires_at=$5 AND refresh_hash=$3 AND csrf_hash=$4 FOR SHARE`,
  body.Tokens.User.ID,body.Tokens.SessionID,HashSecret(body.Tokens.Refresh),HashSecret(body.Tokens.CSRF),body.RefreshExpires).Scan(&session)
 if err!=nil {if err==pgx.ErrNoRows{return Tokens{},denied()};return Tokens{},err}
 if err:=ctx.Err();err!=nil{return Tokens{},err}
 // Recheck wall-clock expiry after potentially waiting on user/session locks.
 // The challenge is locked by the caller throughout this transaction.
 var live bool
 if err:=tx.QueryRow(ctx,`SELECT clock_timestamp() < $1::timestamptz`,receiptExpires).Scan(&live);err!=nil{return Tokens{},err}
 if !live{return Tokens{},denied()}
 if err:=ctx.Err();err!=nil{return Tokens{},err}
 body.Tokens.RefreshExpires=body.RefreshExpires
 return body.Tokens,nil
}

package auth

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

func RequireOTPReceiptSchema(ctx context.Context,db artifactjson.RowQuerier)error{
 if db==nil{return errors.New("OTP recovery database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user='photo_auth'
  AND (SELECT count(*) FROM pg_attribute a
   WHERE a.attrelid=to_regclass('photo.auth_challenges') AND NOT a.attisdropped AND NOT a.attnotnull
    AND (a.attname,a.atttypid) IN (
     ('verify_receipt_binding','bytea'::regtype),('verify_receipt_cipher','bytea'::regtype),
     ('verify_receipt_expires_at','timestamptz'::regtype),('verify_receipt_epoch','bigint'::regtype)))=4
  AND EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid=to_regclass('photo.auth_challenges')
   AND c.conname='auth_verify_receipt_complete' AND c.contype='c' AND c.convalidated)
  AND has_table_privilege(current_user,'photo.auth_challenges','SELECT,UPDATE')
  AND has_table_privilege(current_user,'photo.auth_idempotency_records','SELECT,INSERT')`).Scan(&ready)
 if err!=nil{return err};if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("OTP recovery schema unavailable")};return nil
}

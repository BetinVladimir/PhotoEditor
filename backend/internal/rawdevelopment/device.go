package rawdevelopment

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2capture"
 "photoeditor/backend/internal/v2qualification"
)
// AdmitDevice uses a distinct SQL authority: a device token is never treated as
// a user/editor session. Caller must retain its authenticated tenant transaction.
func (s Source) AdmitDevice(ctx context.Context,tx ReportRows,principal v2capture.DevicePrincipal,shoot,collection uuid.UUID,policy *Policy)(*Policy,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if policy==nil{return nil,nil}
 owned:=*policy
 if !Valid(&owned)||tx==nil||s.Objects==nil||s.Bucket==""||s.ReportID==uuid.Nil||principal.TenantID==uuid.Nil||principal.ProjectID==uuid.Nil||principal.DeviceID==uuid.Nil||principal.CredentialEpoch<1||shoot==uuid.Nil||collection==uuid.Nil{return nil,v2qualification.ErrUnqualified}
 pin:=map[string]string{"model_lock_sha":s.Expected.ModelLockSHA,"runtime_lock_sha":s.Expected.RuntimeLockSHA,"device_profile_sha":s.Expected.DeviceProfileSHA,"dataset_sha":s.Expected.DatasetSHA,"issuer_id":s.Expected.IssuerID.String()}
 capability,err:=s.readQualified(ctx,tx,`SELECT bucket,object_key,object_version,bytes,media_type FROM photo.v25_raw_device_qualification_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,[]any{principal.TenantID,principal.ProjectID,principal.DeviceID,principal.CredentialEpoch,shoot,collection,s.ReportID,s.ReportSHA,pin})
 if err!=nil{return nil,err}
 if !capability.Allows(&owned){return nil,v2qualification.ErrUnqualified}
 if err=ctx.Err();err!=nil{return nil,err}
 return &owned,nil
}

package rawdevelopment

import (
 "context"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2qualification"
)

type ReportRows interface {QueryRow(context.Context,string,...any) pgx.Row}
type ReportObjects interface {Open(context.Context,objectstore.Object)(io.ReadCloser,error)}

// Source pins are deployment-controlled and never assembled from API input.
// Load must run within the caller's authenticated tenant/project transaction.
type Source struct {
 Objects ReportObjects
 Bucket string
 ReportID uuid.UUID
 ReportSHA,DecoderVersion string
 Expected v2qualification.Expected
}

func (s Source) Load(ctx context.Context,tx ReportRows,tenant,project,actor,session uuid.UUID)(*Capability,error) {
 if tx==nil || s.Objects==nil || s.Bucket=="" || s.ReportID==uuid.Nil || tenant==uuid.Nil || project==uuid.Nil || actor==uuid.Nil || session==uuid.Nil {return nil,v2qualification.ErrUnqualified}
 pin:=map[string]string{"model_lock_sha":s.Expected.ModelLockSHA,"runtime_lock_sha":s.Expected.RuntimeLockSHA,
  "device_profile_sha":s.Expected.DeviceProfileSHA,"dataset_sha":s.Expected.DatasetSHA,"issuer_id":s.Expected.IssuerID.String()}
 return s.readQualified(ctx,tx,`SELECT bucket,object_key,object_version,bytes,media_type FROM photo.v25_raw_qualification_object($1,$2,$3,$4,$5,$6,$7)`,[]any{tenant,project,actor,session,s.ReportID,s.ReportSHA,pin})
}

func (s Source) readQualified(ctx context.Context,tx ReportRows,query string,args []any)(*Capability,error){
 // Qualification pins must remain the same across both SQL reads and object IO.
 s.Expected.RequiredChecks=append([]string(nil),s.Expected.RequiredChecks...)
 clonePins:=func(values map[string]struct{})map[string]struct{}{
  if values==nil{return nil}
  owned:=make(map[string]struct{},len(values))
  for value:=range values{owned[value]=struct{}{}}
  return owned
 }
 s.Expected.CalibrationSHAs=clonePins(s.Expected.CalibrationSHAs)
 s.Expected.TestFixtureSHAs=clonePins(s.Expected.TestFixtureSHAs)
 s.Expected.EvidenceSHAs=clonePins(s.Expected.EvidenceSHAs)

 lookup:=func()(objectstore.Object,error){
  if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
  obj:=objectstore.Object{SHA256:s.ReportSHA};var bucket string
  err:=tx.QueryRow(ctx,query,args...).Scan(&bucket,&obj.Key,&obj.VersionID,&obj.Bytes,&obj.ContentType)
  if errors.Is(err,pgx.ErrNoRows){return objectstore.Object{},v2qualification.ErrUnqualified}
  if err!=nil{return objectstore.Object{},err}
  if bucket!=s.Bucket || obj.Key=="" || obj.VersionID=="" || obj.VersionID=="null" || obj.Bytes<2 || obj.Bytes>8*1024*1024 || obj.ContentType!="application/json" {return objectstore.Object{},v2qualification.ErrUnqualified}
  return obj,nil
 }
 obj,err:=lookup();if err!=nil{return nil,err}
 reader,err:=s.Objects.Open(ctx,obj);if err!=nil{return nil,err}
 if reader==nil{return nil,v2qualification.ErrUnqualified}
 // Read at most the authority-declared length plus one byte. A substituted
 // oversized object must not consume the full global report budget.
 if err=ctx.Err();err!=nil{_ = reader.Close();return nil,err}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,obj.Bytes+1));closeErr:=reader.Close()
 if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
 if int64(len(raw))!=obj.Bytes{return nil,v2qualification.ErrUnqualified}
 capability,err:=VerifyCapability(raw,s.ReportSHA,s.DecoderVersion,s.Expected);if err!=nil{return nil,err}
 current,err:=lookup();if err!=nil{return nil,err}
 if current!=obj{return nil,v2qualification.ErrUnqualified}
 if err=ctx.Err();err!=nil{return nil,err}
 return capability,nil
}

// Admit binds an explicit development policy to the current physical
// qualification in the caller's transaction. Call on every admission, including
// idempotency replay; never cache the returned capability across transactions.
// A nil policy preserves the legacy decoder path without advertising settings.
func (s Source) Admit(ctx context.Context,tx ReportRows,tenant,project,actor,session uuid.UUID,policy *Policy)(*Policy,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if policy==nil{return nil,nil}
 // Freeze caller-owned settings before any adapter I/O.
 owned:=*policy
 if !Valid(&owned){return nil,v2qualification.ErrUnqualified}
 capability,err:=s.Load(ctx,tx,tenant,project,actor,session)
 if err!=nil{return nil,err}
 if !capability.Allows(&owned){return nil,v2qualification.ErrUnqualified}
 if err=ctx.Err();err!=nil{return nil,err}
 return &owned,nil
}

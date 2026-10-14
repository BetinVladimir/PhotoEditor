package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type SigningNegativeQualificationReader interface {
 ReadSigningNegativeQualification(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(v2optical.NegativeQualificationPin,error)
}

// Deployment locks are independently operator-approved expectations, never
// reconstructed from the report being verified. All documents are physically
// read under task input/adopted root scope before accepting retained identity.
func ReadSigningNegativeQualificationContent(ctx context.Context,historical SigningNegativeQualificationReader,documents SigningTaskDocumentReader,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask,locks v2qualification.DeploymentLocks)(v2optical.VerifiedNegativeQualification,error){
 var zero v2optical.VerifiedNegativeQualification
 if err:=ctx.Err();err!=nil{return zero,err}
 if historical==nil||documents==nil||task.Operation!="optical.negative"||task.Version!="1"||normalizeCanonicalSigningTask(&task)!=nil{return zero,ErrInvalid}
 locks.RequiredChecks=append([]string(nil),locks.RequiredChecks...)
 locks.CalibrationSHAs=append([]string(nil),locks.CalibrationSHAs...)
 locks.TestFixtureSHAs=append([]string(nil),locks.TestFixtureSHAs...)
 locks.EvidenceSHAs=append([]string(nil),locks.EvidenceSHAs...)
 if _,err:=locks.Expected();err!=nil{return zero,err}
 pin,err:=historical.ReadSigningNegativeQualification(ctx,who,ready,task)
 if err!=nil{return zero,err}
 if _,err:=pin.Compile();err!=nil{return zero,err}
 type witness struct{pin v2input.Ref;raw []byte;maximum int}
 witnesses:=[]witness{}
 read:=func(pin v2input.Ref,maximum int)([]byte,error){
  raw,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,pin,maximum)
  if err!=nil{return nil,err};raw=append([]byte(nil),raw...)
  if !pinned(raw,pin,maximum){return nil,ErrInvalid}
  witnesses=append(witnesses,witness{pin,raw,maximum});return raw,nil
 }
 paramsRaw,err:=read(task.Params,65536);if err!=nil{return zero,err}
 params,err:=v2optical.ParseNegativeParams(paramsRaw);if err!=nil{return zero,err}
 if params.Calibration!=pin.Calibration{return zero,ErrInvalid}
 source,err:=read(params.Base.Source,65536);if err!=nil{return zero,err}
 image,err:=v2optical.VerifyNegativeWorkingImage(params,source);if err!=nil{return zero,err}
 report,err:=read(pin.Report,8<<20);if err!=nil{return zero,err}
 calibration,err:=read(pin.Calibration,65536);if err!=nil{return zero,err}
 verified,err:=v2optical.VerifyNegativeQualificationEvidence(pin,locks,report,calibration,params,image.Color.ICCSHA256)
 if err!=nil{return zero,err}
 for _,witness:=range witnesses{
  current,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,witness.pin,witness.maximum)
  if err!=nil{return zero,err};if !bytes.Equal(current,witness.raw){return zero,ErrInvalid}
 }
 current,err:=historical.ReadSigningNegativeQualification(ctx,who,ready,task)
 if err!=nil{return zero,err};if current!=pin{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return verified,nil
}

var _ SigningNegativeQualificationReader=SQLSigningNegativeQualificationReader{}

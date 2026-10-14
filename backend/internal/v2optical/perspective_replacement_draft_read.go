package v2optical

import (
 "bytes"
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// LoadManualPerspectiveReplacementDraft recovers only an existing preliminary draft. It
// never saves or retries a missing run. The whole current package is recompiled,
// locked and reread before returning owned bytes. Sealed/queued run recovery
// needs the later admission receipt path and is deliberately not inferred here.
func(p PreparedNegativeReplacementInputs)LoadManualPerspectiveReplacementDraft(ctx context.Context,db NegativeTransactions,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,original,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate)(PerspectiveReplacementDraft,error){
 var zero PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||original==uuid.Nil||snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 if _,_,_,err:=p.checked(ctx);err!=nil{return zero,err}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 var expected PerspectiveReplacementDraft
 matches:=0
 for _,step:=range ready{
  if step.Node().OriginalTask!=original{continue}
  matches++
  expected,err=step.CompilePerspectiveReplacementDraft(ctx,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,maxPixels,capacity,nil)
  if err!=nil{return zero,err}
 }
 if matches!=1{return zero,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return zero,err}
 if role!="photo_scheduler"{return zero,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",p.tenant.String());err!=nil{return zero,err}
 read:=func()(string,error){
  var raw *string
  err:=tx.QueryRow(ctx,"SELECT photo.read_v29_manual_perspective_replacement_draft($1,$2,$3,$4,$5,$6,$7,$8)",p.tenant,p.project,p.who.UserID,p.who.SessionID,p.review,p.hash,original,snapshot).Scan(&raw)
  if err!=nil{return "",err}
  if raw==nil||len(*raw)<2||len(*raw)>3<<20||!bytes.Equal([]byte(*raw),expected.Bytes()){return "",v2input.ErrExpired}
  return *raw,nil
 }
 raw,err:=read();if err!=nil{return zero,err}
 recovered,err:=p.LockManualPerspectiveReplacementDraft(ctx,tx,rebuilt,retained,mappings,unchanged,[]byte(raw),snapshot,located,locks,maxPixels,capacity);if err!=nil{return zero,err}
 if _,err=read();err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return recovered,nil
}

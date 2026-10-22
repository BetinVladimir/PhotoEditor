package v2region

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Recovery does not impersonate a revoked actor. SQL returns immutable intent
// to the scheduler only; every creation still uses normal live admission.
// Cancellation remains possible after draft/session authority is withdrawn.
type AuthoredRegionRecovery struct { Scheduler *pgxpool.Pool; Children AuthoredRegionChildren }
func(s AuthoredRegionRecovery)ProcessOne(ctx context.Context)(worked bool,resultErr error){
 if s.Children==nil{return false,ErrRegionUnavailable}
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return false,err}
 token:=uuid.New();var r AuthoredRegionRecord;var tenant uuid.UUID;var raw []byte
 err:=s.Scheduler.QueryRow(ctx,"SELECT tenant_id,request_id,intent_jcs,intent_sha,render_batch_id,crop_run_id,cancel_requested,revision,is_current FROM photo.claim_authored_region_recovery($1)",token).Scan(&tenant,&r.ID,&raw,&r.SHA,&r.RenderBatch,&r.CropRun,&r.CancelRequested,&r.Revision,&r.Current)
 if errors.Is(err,pgx.ErrNoRows){return false,nil};if err!=nil{return false,err}
 worked=true
 // Best-effort release has its own short bound and cannot replace the actual
 // operation error. A crash leaves a bounded lease, never a refund authority.
 defer func(){
  cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer cancel()
  var released bool
  e:=s.Scheduler.QueryRow(cleanup,"SELECT photo.finish_authored_region_recovery($1,$2,$3,$4)",tenant,r.ID,token,resultErr!=nil).Scan(&released)
  if resultErr==nil{if e!=nil{resultErr=e}else if !released{resultErr=v2input.ErrConflict}}
 }()
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw)||json.Unmarshal(raw,&r.Intent)!=nil{return true,ErrRegionInvalid}
 if r.Intent.Tenant!=tenant{return true,ErrRegionInvalid};if err=r.validate();err!=nil{return true,err}
 f,err:=s.Children.Inspect(ctx,r);if err!=nil{return true,err}
 phase,err:=authoredPhase(r,f);if err!=nil{return true,err}
 switch phase{
 case AuthoredRegionCancelling:return true,s.Children.Cancel(ctx,r)
 case AuthoredRegionAdmitted:return true,s.Children.StartRender(ctx,r)
 case AuthoredRegionCropping:
  if f.RenderCanonical&&!f.CropExists{return true,s.Children.StartCrop(ctx,r)}
 }
 return true,nil
}

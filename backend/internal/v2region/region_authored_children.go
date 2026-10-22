package v2region

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)

// Verification is an independently replayed physical result, not a worker
// acknowledgement or a callback success flag. The identity must still match
// the exact current request and canonical SQL result after object IO.
type AuthoredCropVerification struct {
 Request,Run,Task,Result,Root uuid.UUID
 IntentSHA,RootSHA string
 Attempt int
 Fence int64
}
type AuthoredCropVerifier interface {
 Verify(context.Context,AuthoredRegionRecord)(AuthoredCropVerification,error)
}

type SQLAuthoredRegionChildren struct {
 Scheduler *pgxpool.Pool
 Render AuthoredRegionRender
 Evidence SQLAuthoredRenderEvidence
 Params RegionParamsPublisher
 Documents v2input.DocumentPublisher
 // Profile is trusted installed server configuration. PrepareCrop rejects
 // transport decorations and derives resources from the actual input closure.
 Profile []byte
 CropVerifier AuthoredCropVerifier
}

func(s SQLAuthoredRegionChildren)StartRender(ctx context.Context,r AuthoredRegionRecord)error{
 if err:=r.validate();err!=nil{return err}
 if s.Render.Scheduler!=s.Scheduler{return ErrRegionInvalid}
 return s.Render.Start(ctx,r)
}
func(s SQLAuthoredRegionChildren)StartCrop(ctx context.Context,r AuthoredRegionRecord)error{
 if err:=r.validate();err!=nil{return err}
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return err}
 if s.Evidence.Scheduler!=s.Scheduler||s.Params==nil||s.Documents==nil{return ErrRegionUnavailable}
 if !r.Current||r.CancelRequested{return v2input.ErrExpired}
 node,err:=s.Evidence.PrepareCrop(ctx,r,s.Params,append([]byte(nil),s.Profile...));if err!=nil{return err}
 // The real snapshot function repeats live request authority. Child admission
 // repeats it again before and after transport publication, without an IO TX.
 var snapshot uuid.UUID
 if err=s.Scheduler.QueryRow(ctx,"SELECT photo.freeze_authored_region_render_snapshot($1,$2,$3)",r.Intent.Tenant,r.ID,r.SHA).Scan(&snapshot);err!=nil{return err}
 if snapshot==uuid.Nil{return v2input.ErrExpired}
 ticket,err:=node.Admit(ctx,s.Scheduler,v2input.ScopedPublisher{Documents:s.Documents,Tenant:r.Intent.Tenant,Run:r.CropRun},snapshot);if err!=nil{return err}
 if ticket.RunID!=r.CropRun{return ErrRegionInvalid};return nil
}
func(s SQLAuthoredRegionChildren)Inspect(ctx context.Context,r AuthoredRegionRecord)(AuthoredRegionChildFacts,error){
 var f AuthoredRegionChildFacts
 if err:=r.validate();err!=nil{return f,err}
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return f,err}
 var cropTask,cropResult,cropRoot uuid.UUID;var attempt int;var fence int64;var rootSHA string
 err:=s.Scheduler.QueryRow(ctx,"SELECT render_exists,render_terminal,render_canonical,crop_exists,crop_terminal,crop_canonical,writers_closed,quota_settled,crop_task,crop_result,crop_attempt,crop_fence,crop_root_id,crop_root_sha FROM photo.inspect_authored_region_children($1,$2,$3)",r.Intent.Tenant,r.ID,r.SHA).Scan(
  &f.RenderExists,&f.RenderTerminal,&f.RenderCanonical,&f.CropExists,&f.CropTerminal,&f.CropCanonical,&f.AllWritersClosed,&f.AllQuotaSettled,&cropTask,&cropResult,&attempt,&fence,&cropRoot,&rootSHA)
 if err!=nil{return AuthoredRegionChildFacts{},err}
 if f.CropCanonical{
  if s.CropVerifier==nil{return AuthoredRegionChildFacts{},ErrRegionUnavailable}
  proof,e:=s.CropVerifier.Verify(ctx,r);if e!=nil{return AuthoredRegionChildFacts{},e}
  if proof.Request!=r.ID||proof.Run!=r.CropRun||proof.IntentSHA!=r.SHA||proof.Task!=cropTask||proof.Result!=cropResult||proof.Attempt!=attempt||proof.Fence!=fence||proof.Root!=cropRoot||proof.RootSHA!=rootSHA{return AuthoredRegionChildFacts{},ErrRegionInvalid}
  // Canonical identity and authority must survive the physical replay. Never
  // issue ready from the first SQL read when cancellation or adoption changed.
  var same bool
  if e=s.Scheduler.QueryRow(ctx,"SELECT photo.authored_region_crop_identity_current($1,$2,$3,$4,$5,$6,$7,$8,$9)",r.Intent.Tenant,r.ID,r.SHA,cropTask,cropResult,attempt,fence,cropRoot,rootSHA).Scan(&same);e!=nil{return AuthoredRegionChildFacts{},e}
  if !same{return AuthoredRegionChildFacts{},v2input.ErrExpired}
 }
 return f,nil
}
func(s SQLAuthoredRegionChildren)Cancel(ctx context.Context,r AuthoredRegionRecord)error{
 if err:=r.validate();err!=nil{return err}
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return err}
 var ok bool
 // Scheduler cleanup derives only these immutable owned children. It must
 // remain usable after session withdrawal; it does not impersonate the actor
 // or release quota while any writer can still publish.
 if err:=s.Scheduler.QueryRow(ctx,"SELECT photo.cancel_authored_region_children($1,$2,$3)",r.Intent.Tenant,r.ID,r.SHA).Scan(&ok);err!=nil{return err}
 if !ok{return v2input.ErrExpired};return nil
}

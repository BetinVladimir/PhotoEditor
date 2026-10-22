package v2region

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type authoredRenderIdentity struct{Run,Task,Result uuid.UUID;Attempt int;Fence int64;Root v2input.Ref}
// SQLAuthoredRenderEvidence follows only this compound request's one qualified
// render part. Generic same-project artifact access is deliberately unavailable.
// Physical IO occurs outside both API and scheduler transactions; every object
// tuple and the current canonical identity are rechecked after the reads.
type SQLAuthoredRenderEvidence struct{API,Scheduler *pgxpool.Pool;Objects RegionObjectReader;Bucket string;Store AuthoredRegionStore}
func(s SQLAuthoredRenderEvidence)identity(ctx context.Context,r AuthoredRegionRecord)(authoredRenderIdentity,error){
 var out authoredRenderIdentity;var role string
 if s.Scheduler==nil{return out,ErrRegionUnavailable}
 if err:=s.Scheduler.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return out,err}
 if role!="photo_scheduler"{return out,ErrRegionInvalid}
 err:=s.Scheduler.QueryRow(ctx,"SELECT * FROM photo.authored_region_render_result($1,$2)",r.Intent.Tenant,r.ID).Scan(&out.Run,&out.Task,&out.Result,&out.Attempt,&out.Fence,&out.Root.ArtifactID,&out.Root.SHA256)
 if err!=nil{return out,err}
 workflow,_:=v2input.WorkflowID(out.Run);task,_:=v2input.TaskID(workflow,"render.compose",0)
 if out.Run!=uuid.NewSHA1(r.RenderBatch,[]byte("render.batch:0"))||out.Task!=task||out.Result==uuid.Nil||out.Attempt<1||out.Attempt>6||out.Fence<1||out.Root.ArtifactID==uuid.Nil||!validRegionSHA(out.Root.SHA256){return authoredRenderIdentity{},ErrRegionInvalid}
 return out,nil
}
func(s SQLAuthoredRenderEvidence)metadata(ctx context.Context,r AuthoredRegionRecord,ref v2input.Ref)(objectstore.Object,error){
 var out objectstore.Object;var project,id uuid.UUID;var sha,bucket string
 if s.API==nil||s.Bucket==""||ref.ArtifactID==uuid.Nil||!validRegionSHA(ref.SHA256){return out,ErrRegionInvalid}
 err:=database.TenantTransaction(ctx,s.API,r.Intent.Tenant,func(tx pgx.Tx)error{
  var role string;if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_api"{return ErrRegionInvalid}
  return tx.QueryRow(ctx,"SELECT * FROM photo.authored_region_render_object($1,$2,$3,$4,$5,$6)",r.Intent.Tenant,r.ID,r.Intent.Actor,r.Intent.Session,ref.ArtifactID,ref.SHA256).Scan(&project,&id,&sha,&bucket,&out.Key,&out.VersionID,&out.Bytes,&out.ContentType)
 });if err!=nil{return out,err}
 if project!=r.Intent.Project||id!=ref.ArtifactID||sha!=ref.SHA256||bucket!=s.Bucket||out.Key==""||out.VersionID==""||out.VersionID=="null"||out.Bytes<2||out.Bytes>9007199254740991{return objectstore.Object{},ErrRegionInvalid}
 out.SHA256=sha;return out,nil
}
func(s SQLAuthoredRenderEvidence)Load(ctx context.Context,r AuthoredRegionRecord)(AuthoredCropPlan,error){
 var empty AuthoredCropPlan
 if s.Store==nil||s.Objects==nil||r.validate()!=nil||!r.Current||r.CancelRequested{return empty,ErrRegionInvalid}
 before,err:=s.identity(ctx,r);if err!=nil{return empty,err}
 type read struct{ref v2input.Ref;object objectstore.Object;body []byte};reads:=[]read{}
 get:=func(ref v2input.Ref)([]byte,error){
  // A lawful no-op render may use the original WorkingImage as its result.
  // Deduplicate exact refs while refusing one ID rebound to different bytes.
  for _,prior:=range reads{if prior.ref.ArtifactID==ref.ArtifactID{if prior.ref!=ref{return nil,ErrRegionInvalid};return append([]byte(nil),prior.body...),nil}}
  object,e:=s.metadata(ctx,r,ref);if e!=nil{return nil,e};body,e:=readAuthoredRegionDocument(ctx,s.Objects,object,ref);if e!=nil{return nil,e}
  reads=append(reads,read{ref,object,body});return append([]byte(nil),body...),nil
 }
 recipe,err:=get(r.Intent.Recipe);if err!=nil{return empty,err}
 source,err:=get(r.Intent.Binding.Source);if err!=nil{return empty,err}
 root,err:=get(before.Root);if err!=nil{return empty,err}
 parsed,err:=v2draft.ParseImageResult(root,before.Root);if err!=nil{return empty,err}
 image,err:=get(parsed.WorkingImage);if err!=nil{return empty,err}
 report,err:=get(parsed.Report);if err!=nil{return empty,err}
 plan,err:=CompileAuthoredCropPlan(r,before.Run,before.Root,recipe,source,root,image,report);if err!=nil{return empty,err}
 for _,prior:=range reads{latest,e:=s.metadata(ctx,r,prior.ref);if e!=nil{return empty,e};if latest!=prior.object{return empty,v2input.ErrConflict}}
 after,err:=s.identity(ctx,r);if err!=nil{return empty,err};if after!=before{return empty,v2input.ErrConflict}
 current,err:=s.Store.Read(ctx,auth.Identity{UserID:r.Intent.Actor,SessionID:r.Intent.Session},r.Intent.Tenant,r.Intent.Asset,r.ID)
 if err!=nil{return empty,err};if current.validate()!=nil{return empty,ErrRegionInvalid}
 if current.SHA!=r.SHA||current.RenderBatch!=r.RenderBatch||current.CropRun!=r.CropRun||current.Revision!=r.Revision{return empty,v2input.ErrConflict}
 if !current.Current||current.CancelRequested{return empty,v2input.ErrExpired}
 return plan,nil
}

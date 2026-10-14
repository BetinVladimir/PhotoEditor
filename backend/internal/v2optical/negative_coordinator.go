package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Deployment owns qualification policy and capacity; HTTP callers provide only
// the intent and the accepted source snapshot, never resource/policy overrides.
type NegativeCoordinator struct {
 DB NegativeTransactions
 Reader v2input.SessionAnalysisObjectReader
 Writer artifactjson.Objects
 Bucket string
 Pin NegativeQualificationPin
 Locks v2qualification.DeploymentLocks
 Capacity NegativeResourceEstimate
}
type NegativePrepareRequest struct {
 Tenant,Project,Snapshot uuid.UUID
 Key string
 Params,Source []byte
}
type PreparedNegative struct {
 draft NegativeDraft
 params artifactjson.Ref
 qualification LocatedNegativeQualification
}
func(p PreparedNegative) Draft() NegativeDraft { return NegativeDraft{raw:p.draft.raw} }
func(p PreparedNegative) Params() artifactjson.Ref { return p.params }

func negativeRequestCapacity(required,capacity NegativeResourceEstimate)(NegativeResourceEstimate,error) {
 if required.PixelCount<1 || capacity.PixelCount<required.PixelCount || capacity.PixelCount>200000000 || capacity.VRAMBytes!=0 { return NegativeResourceEstimate{},v2input.ErrInvalid }
 have:=[]int64{capacity.RAMBytes,capacity.MaterialScratchBytes,capacity.OutputScratchBytes,capacity.ScratchBytes,capacity.OutputBytes}
 need:=[]int64{required.RAMBytes,required.MaterialScratchBytes,required.OutputScratchBytes,required.ScratchBytes,required.OutputBytes}
 for i,n:=range have { if n<need[i] || n>9007199254740991 { return NegativeResourceEstimate{},v2input.ErrInvalid } }
 if capacity.ScratchBytes<capacity.MaterialScratchBytes+capacity.OutputScratchBytes { return NegativeResourceEstimate{},v2input.ErrInvalid }
 capacity.PixelCount=required.PixelCount
 return capacity,nil
}

func(c NegativeCoordinator) compileRequest(ctx context.Context,who auth.Identity,r NegativePrepareRequest)(PreparedNegative,error) {
 var zero PreparedNegative
 if err:=ctx.Err();err!=nil { return zero,err }
 if c.DB==nil || c.Reader==nil || len(c.Bucket)<3 || len(c.Bucket)>63 || r.Tenant==uuid.Nil || r.Project==uuid.Nil || r.Snapshot==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || !idempotency.ValidKey(r.Key) { return zero,v2input.ErrInvalid }
 r.Params=append([]byte(nil),r.Params...);r.Source=append([]byte(nil),r.Source...)
 params,err:=ParseNegativeParams(r.Params);if err!=nil { return zero,err }
 image,err:=VerifyNegativeWorkingImage(params,r.Source);if err!=nil { return zero,err }
 required,err:=NegativeResources(image.Canvas.Width,image.Canvas.Height,image.Alpha!=nil);if err!=nil { return zero,err }
 capacity,err:=negativeRequestCapacity(required,c.Capacity);if err!=nil { return zero,err }
 located,err:=LoadNegativeQualificationEvidence(ctx,SQLNegativeQualificationCatalog{DB:c.DB,Bucket:c.Bucket},c.Reader,who,r.Tenant,r.Project,c.Pin,c.Locks,params,image.Color.ICCSHA256);if err!=nil { return zero,err }
 qualified,err:=located.Qualification();if err!=nil { return zero,err }
 draft,err:=CompileNegativeDraft(ctx,who,r.Tenant,r.Project,r.Snapshot,r.Key,r.Params,r.Source,qualified,capacity);if err!=nil { return zero,err }
 snapshot,err:=draft.Snapshot();if err!=nil { return zero,err }
 return PreparedNegative{draft:draft,params:artifactjson.Ref{ID:snapshot.ParamsDocument,SHA256:snapshot.ParamsSHA},qualification:located},nil
}

func(c NegativeCoordinator) PrepareParams(ctx context.Context,who auth.Identity,r NegativePrepareRequest)(PreparedNegative,error) {
 var zero PreparedNegative
 if c.Writer==nil { return zero,v2input.ErrInvalid }
 prepared,err:=c.compileRequest(ctx,who,r);if err!=nil { return zero,err }
 qualified,err:=prepared.qualification.Qualification();if err!=nil { return zero,err }
 plan,_,source,err:=prepared.draft.RestorePlan(ctx,qualified);if err!=nil { return zero,err }
 params,err:=ParseNegativeParams(plan.Params);if err!=nil { return zero,err }
 if err:=VerifyNegativeSourceObjects(ctx,SQLNegativeSourceArrayCatalog{DB:c.DB,Bucket:c.Bucket},c.Reader,who,r.Tenant,r.Project,r.Snapshot,params,source);err!=nil { return zero,err }
 published,err:=PublishNegativeParams(ctx,c.DB,c.Writer,c.Bucket,who,r.Tenant,prepared.draft,prepared.qualification);if err!=nil { return zero,err }
 if published!=prepared.params { return zero,v2input.ErrInvalid }
 return prepared,nil
}

func(p PreparedNegative) bind(ctx context.Context,who auth.Identity,r NegativePrepareRequest)error {
 if err:=ctx.Err();err!=nil { return err }
 snapshot,err:=p.draft.Snapshot();if err!=nil { return err }
 if !idempotency.ValidKey(r.Key) || snapshot.Owner.TenantID!=r.Tenant || snapshot.Owner.ProjectID!=r.Project || snapshot.Owner.SnapshotID!=r.Snapshot || snapshot.Owner.ActorID!=who.UserID || snapshot.Owner.AuthSessionID!=who.SessionID || snapshot.Owner.RunID!=uuid.NewSHA1(r.Tenant,[]byte("V29.Negative:"+r.Project.String()+":"+who.UserID.String()+":"+r.Key)) { return v2input.ErrInvalid }
 qualified,err:=p.qualification.Qualification();if err!=nil { return err }
 plan,params,source,err:=p.draft.RestorePlan(ctx,qualified);if err!=nil { return err }
 if string(plan.Params)!=string(r.Params) || string(source)!=string(r.Source) || p.params.ID!=params.ArtifactID || p.params.SHA256!=params.SHA256 { return v2input.ErrInvalid }
 return nil
}
func(c NegativeCoordinator) PublishTransport(ctx context.Context,who auth.Identity,r NegativePrepareRequest,p PreparedNegative)(v2input.GraphTask,error) {
 if err:=p.bind(ctx,who,r);err!=nil { return v2input.GraphTask{},err }
 return PublishNegativeTaskTransport(ctx,c.DB,c.Writer,c.Bucket,who,r.Tenant,p.draft,p.qualification,v2input.Ref{ArtifactID:p.params.ID,SHA256:p.params.SHA256})
}
func(c NegativeCoordinator) Admit(ctx context.Context,who auth.Identity,r NegativePrepareRequest,p PreparedNegative)error {
 if err:=p.bind(ctx,who,r);err!=nil { return err }
 _,err:=AdmitNegativeGraph(ctx,c.DB,c.Reader,c.Bucket,who,r.Tenant,p.draft,p.qualification,v2input.Ref{ArtifactID:p.params.ID,SHA256:p.params.SHA256})
 return err
}
func(c NegativeCoordinator) ReadQueuedReceipt(ctx context.Context,who auth.Identity,r NegativePrepareRequest)(NegativeReceipt,error) {
 p,err:=c.compileRequest(ctx,who,r);if err!=nil { return NegativeReceipt{},err }
 return ReadNegativeQueuedReceipt(ctx,c.DB,c.Reader,c.Bucket,who,r.Tenant,p.draft,p.qualification,v2input.Ref{ArtifactID:p.params.ID,SHA256:p.params.SHA256})
}

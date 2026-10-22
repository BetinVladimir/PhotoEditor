package v2region

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2render"
)

// SQLAuthoredRegionPolicy only selects an actually imported renderer and a
// verified calibration. It neither creates qualification nor enables workers.
type SQLAuthoredRegionPolicy struct {
 Resolver v2render.SQLRenderQualificationResolver
 Calibrations v2render.RenderCalibrationCatalog
 TargetICCSHA string
 PresentationRuntimeSHA,PresentationICCLockSHA string
}
func(p SQLAuthoredRegionPolicy) Resolve(ctx context.Context,who auth.Identity,tenant uuid.UUID,prepared PreparedAuthoredRegion)(AuthoredRegionIntent,error){
 var empty AuthoredRegionIntent
 if p.Calibrations==nil||!validRegionSHA(p.TargetICCSHA)||!validRegionSHA(p.PresentationRuntimeSHA)||!validRegionSHA(p.PresentationICCLockSHA){return empty,ErrRegionUnavailable}
 q,err:=p.Resolver.Resolve(ctx,who,tenant,prepared.Project);if err!=nil{return empty,err}
 calibration,err:=p.Calibrations.Resolve(ctx,q);if err!=nil{return empty,err}
 // Select invokes the real calibration validator and covering resource checks;
 // a merely syntactic report or backend string is insufficient.
 if _,err=calibration.Select(q,int64(prepared.Evidence.Source.Canvas.Width),int64(prepared.Evidence.Source.Canvas.Height),prepared.Evidence.Recipe.Scalars.ICCLockSHA);err!=nil{return empty,err}
 calibrationSHA,err:=canonical.Hash(calibration);if err!=nil{return empty,err}
 policy,err:=v2render.DefaultRenderPolicy();if err!=nil{return empty,err}
 current,err:=p.Resolver.Resolve(ctx,who,tenant,prepared.Project);if err!=nil{return empty,err};if current!=q{return empty,ErrRegionUnavailable}
 after,err:=p.Calibrations.Resolve(ctx,q);if err!=nil{return empty,err}
 afterSHA,err:=canonical.Hash(after);if err!=nil||afterSHA!=calibrationSHA{return empty,ErrRegionUnavailable}
 return AuthoredRegionIntent{Tenant:tenant,Project:prepared.Project,Actor:who.UserID,Session:who.SessionID,
 Binding:prepared.Binding,TargetICCSHA:p.TargetICCSHA,PresentationRuntimeSHA:p.PresentationRuntimeSHA,PresentationICCLockSHA:p.PresentationICCLockSHA,RenderPolicySHA:policy.PolicySHA,
 Qualification:v2render.RenderQualificationPin{RenderQualification:q,CalibrationSHA:calibrationSHA}},nil
}

type AuthoredRegionQueue struct {
 Preparation SQLAuthoredRegionPreparation
 Policy SQLAuthoredRegionPolicy
 Coordinator AuthoredRegionCoordinator
}
func(q AuthoredRegionQueue) Queue(ctx context.Context,who auth.Identity,tenant,asset uuid.UUID,key string,request AuthoredRegionRequest)(AuthoredRegionTicket,error){
 var empty AuthoredRegionTicket
 if q.Coordinator.Store==nil||q.Coordinator.Children==nil{return empty,ErrRegionUnavailable}
 prepared,err:=q.Preparation.Prepare(ctx,who,tenant,asset,request);if err!=nil{return empty,err}
 intent,err:=q.Policy.Resolve(ctx,who,tenant,prepared);if err!=nil{return empty,err}
 intent.Asset=asset;intent.DraftDocument=request.DraftDocument;intent.DraftRevision=request.DraftRevision
 intent.FrozenDocument=request.FrozenDocument;intent.Recipe.ArtifactID=request.Preview.Recipe.ArtifactID;intent.Recipe.SHA256=request.Preview.Recipe.SHA256
 intent.Rect=request.Preview.Rect;intent.MaxEdge=request.Preview.MaxEdge
 if err=request.ValidateIntent(intent);err!=nil{return empty,err}
 if _,err=intent.SHA256();err!=nil{return empty,err}
 saved,err:=q.Coordinator.Store.Begin(ctx,intent,key);if err!=nil{return empty,err}
 return q.Coordinator.Reconcile(ctx,who,tenant,asset,saved.ID)
}

func(q AuthoredRegionQueue) Status(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionTicket,error){
 return q.Coordinator.Reconcile(ctx,who,tenant,asset,id)
}
func(q AuthoredRegionQueue) Cancel(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionTicket,error){
 return q.Coordinator.Cancel(ctx,who,tenant,asset,id)
}

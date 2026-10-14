package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type AutomaticPerspectiveReplacementQualification struct {
 Located LocatedPerspectiveQualification
 Locks v2qualification.DeploymentLocks
 MaxPixels int64
 Capacity PerspectiveResourceEstimate
}
// Resolve exact deployment-provisioned project, fresh rebound user controls and
// source dimensions. No unqualified/default project or resource fallback.
func(p *PerspectiveProjects) PrepareAutomaticReplacementQualification(ctx context.Context,step PreparedMappedReplacementStep,lines v2input.Ref,raw []byte)(AutomaticPerspectiveReplacementQualification,error){
 var zero AutomaticPerspectiveReplacementQualification
 if err:=ctx.Err();err!=nil{return zero,err}
 source,err:=step.PerspectiveDetectionSource(ctx);if err!=nil{return zero,err}
 if p==nil{return zero,v2qualification.ErrUnqualified}
 service:=p.services[[2]uuid.UUID{source.Tenant,source.Project}]
 if service==nil{return zero,v2qualification.ErrUnqualified}
 c:=service.coordinator
 if c.DB==nil||c.Reader==nil{return zero,v2input.ErrInvalid}
 intent,err:=step.Rebind(ctx,&lines);if err!=nil{return zero,err}
 params,err:=ParsePerspectiveParams(intent.Params());if err!=nil{return zero,err}
 if params.Base!=source.Base{return zero,v2input.ErrExpired}
 segments,err:=VerifyPerspectiveLinesOriginal(params,raw);if err!=nil{return zero,err}
 image,err:=VerifyPerspectiveWorkingImage(params,source.WorkingImage);if err!=nil{return zero,err}
 geometry,err:=CompilePerspectiveAutoGeometry(ctx,params,segments,image.Canvas.Width,image.Canvas.Height,c.MaxPixels);if err!=nil{return zero,err}
 required,err:=PerspectiveResources(image.Canvas.Width,image.Canvas.Height,geometry.Width,geometry.Height);if err!=nil{return zero,err}
 if required.PixelCount>c.Capacity.PixelCount{return zero,v2input.ErrInvalid}
 capacity:=c.Capacity;capacity.PixelCount=required.PixelCount
 if err=ValidatePerspectiveCapacity(required,capacity);err!=nil{return zero,err}
 located,err:=LoadPerspectiveQualificationEvidence(ctx,SQLPerspectiveQualificationCatalog{DB:c.DB,Bucket:c.Bucket},c.Reader,source.Principal,source.Tenant,source.Project,c.Pin,c.Locks,params,image.Color.ICCSHA256);if err!=nil{return zero,err}
 return AutomaticPerspectiveReplacementQualification{Located:located,Locks:c.Locks,MaxPixels:c.MaxPixels,Capacity:capacity},ctx.Err()
}

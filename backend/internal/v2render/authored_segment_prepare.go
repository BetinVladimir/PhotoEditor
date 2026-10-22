package v2render

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// SQLAuthoredSegmentPreparation connects real published current documents to
// the distinct qualified segment compiler. It never reuses final/2 calibration
// or returns a fabricated future Params ref. Stage/transport/seal are separate
// actual transactions, and must repeat the current protected instruction.
type SQLAuthoredSegmentPreparation struct {
 API *pgxpool.Pool
 Params AuthoredSegmentParamsPublication
 PhaseObjects SQLAuthoredPhaseObjects
 Calibrations AuthoredSegmentCalibrationCatalog
}

func(s SQLAuthoredSegmentPreparation)Prepare(ctx context.Context,who auth.Identity,tenant,parent uuid.UUID,ordinal int,currentLayers,protected *v2input.Ref)(AuthoredSegmentTaskPlan,error){
 empty:=AuthoredSegmentTaskPlan{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if s.API==nil||s.Params.Scheduler==nil||s.Params.Objects==nil||s.Calibrations==nil||s.PhaseObjects.API!=s.API||s.PhaseObjects.Objects==nil||s.Params.Bucket==""||s.PhaseObjects.Bucket!=s.Params.Bucket{return empty,v2input.ErrInvalid}
 before,present,err:=ReadAuthoredSegmentInventory(ctx,s.Params.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !present{return empty,v2input.ErrExpired}
 source:=before.Context
 if source.Actor!=who.UserID||source.Session!=who.SessionID{return empty,v2input.ErrInvalid}
 var base struct{Original v2input.SessionVersionBinding `json:"original_source"`}
 if json.Unmarshal([]byte(before.BaseReceiptJCS),&base)!=nil||!authoredBindingValid(base.Original){return empty,v2input.ErrInvalid}
 item:=RenderSelectionItem{Binding:base.Original,Recipe:source.OriginalRecipe}
 calibrated,err:=LoadCalibratedAuthoredSegmentRequest(ctx,s.API,s.PhaseObjects.Objects,s.Params.Bucket,s.Calibrations,tenant,source.Project,who,item)
 if err!=nil{return empty,err};if calibrated.OperationVersion!="1"{return empty,v2input.ErrInvalid}
 paramsRef,published,err:=s.Params.Publish(ctx,tenant,parent,ordinal,currentLayers,protected)
 if err!=nil{return empty,err}
 // The separately reviewed scoped evidence resolver must read these actual
 // published bytes, retaining original bootstrap and each current typed edge.
 evidence,err:=ReadAuthoredSegmentInputEvidence(ctx,s.Params.Scheduler,s.PhaseObjects,who,tenant,parent,ordinal,paramsRef)
 if err!=nil{return empty,err};if !bytes.Equal(evidence.Params.JSON,published.JSON){return empty,v2input.ErrInvalid}
 target,err:=s.PhaseObjects.ReadJSON(ctx,who,tenant,source.Project,parent,ordinal,published.Params.Source.Source)
 if err!=nil{return empty,err}
 var currentRaw []byte
 if published.Params.CurrentLayers!=nil{
  currentRaw,err=s.PhaseObjects.ReadJSON(ctx,who,tenant,source.Project,parent,ordinal,*published.Params.CurrentLayers)
  if err!=nil{return empty,err}
 }
 calibration,err:=s.Calibrations.ResolveAuthoredSegment(ctx,calibrated.Qualification)
 if err!=nil{return empty,err}
 calibrationSHA,err:=canonical.Hash(calibration)
 if err!=nil{return empty,err};if calibrationSHA!=calibrated.CalibrationSHA{return empty,renderUnavailable()}
 profile,err:=CompileAuthoredSegmentExecutionProfile(calibrated.Request,published,target,currentRaw,calibrated.Qualification,calibration)
 if err!=nil{return empty,err}
 plan,err:=compileAuthoredSegmentTask(evidence,paramsRef,profile);if err!=nil{return empty,err}
 current,err:=(SQLAuthoredSegmentQualificationResolver{API:s.API}).Resolve(ctx,who,tenant,source.Project)
 if err!=nil{return empty,err};if current!=calibrated.Qualification{return empty,renderUnavailable()}
 calibration,err=s.Calibrations.ResolveAuthoredSegment(ctx,current)
 if err!=nil{return empty,err};if err=calibration.validate(current);err!=nil{return empty,err}
 calibrationSHA,err=canonical.Hash(calibration)
 if err!=nil{return empty,err};if calibrationSHA!=calibrated.CalibrationSHA{return empty,renderUnavailable()}
 after,present,err:=ReadAuthoredSegmentInventory(ctx,s.Params.Scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,err};if !present{return empty,v2input.ErrExpired}
 a,err:=canonical.Marshal(before);if err!=nil{return empty,err};b,err:=canonical.Marshal(after)
 if err!=nil{return empty,err};if !bytes.Equal(a,b){return empty,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return empty,err};return plan,nil
}

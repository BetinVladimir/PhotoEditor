package delivery

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// ReauthorizeWatermarkPin repeats current source authority and physically reads
// the exact pinned manifest. No latest-version lookup is permitted on recovery.
// Array descriptors and physical NPY bytes are independently reverified.
func(s Service)ReauthorizeWatermarkPin(ctx context.Context,who auth.Identity,tid uuid.UUID,scope WatermarkPinScope,configRaw []byte,pin FrozenWatermarkSource,bucket string,objects v2input.SessionAnalysisObjectReader)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 if err:=ctx.Err();err!=nil{return zero,err}
 if tid==uuid.Nil||scope.Tenant!=tid||scope.Actor!=who.UserID||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||objects==nil{return zero,v2input.ErrInvalid}
 owned,err:=DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,configRaw);if err!=nil{return zero,err}
 var envelope watermarkPinEnvelope
 if json.Unmarshal(owned.body,&envelope)!=nil{return zero,v2input.ErrInvalid}
 source,err:=s.ResolveWatermarkSource(ctx,who,tid,scope.Project,envelope.Asset,envelope.Binding.AssetVersionID,bucket)
 if err!=nil{return zero,err}
 if source.Ref!=envelope.Binding.Source{return zero,v2input.ErrExpired}
 evidence,err:=LoadWatermarkSourceEvidence(ctx,who,tid,source,objects,v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:bucket,ProjectID:scope.Project,AssetVersionID:source.Version})
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if err=VerifyWatermarkSourceArrays(ctx,objects,evidence);err!=nil{return zero,err}
 return RestoreWatermarkSourcePin(owned.body,owned.pin,scope,configRaw,evidence)
}

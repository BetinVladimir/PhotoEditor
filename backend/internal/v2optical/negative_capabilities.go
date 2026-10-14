package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// NegativeCapabilities exposes configured input choices after current catalogue
// authorization. It is not physical qualification evidence or admission authority.
type NegativeCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Calibration v2input.Ref `json:"calibration"`
 MaxPixels int64 `json:"max_pixels"`
}
func(p *NegativeProjects) ProjectCapabilities(ctx context.Context,who auth.Identity,tenant,project uuid.UUID)(NegativeCapabilities,error) {
 var zero NegativeCapabilities
 if err:=ctx.Err();err!=nil { return zero,err }
 if p==nil { return zero,v2qualification.ErrUnqualified }
 service:=p.services[[2]uuid.UUID{tenant,project}]
 if service==nil { return zero,v2qualification.ErrUnqualified }
 c:=service.coordinator
 if _,_,_,err:=(SQLNegativeQualificationCatalog{DB:c.DB,Bucket:c.Bucket}).ResolveNegativeQualification(ctx,who,tenant,project,c.Pin);err!=nil { return zero,err }
 return NegativeCapabilities{SchemaVersion:1,Tenant:tenant,Project:project,Calibration:c.Pin.Calibration,MaxPixels:c.Capacity.PixelCount},nil
}

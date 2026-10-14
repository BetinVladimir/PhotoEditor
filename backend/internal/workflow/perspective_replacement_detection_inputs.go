package workflow

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// This witness binds stream verification to one exact replacement descriptor.
// Catalogs must resolve the actual mapped source; ordinary snapshot-only source
// catalogs cannot authorize replacement-produced objects. SQL must repeat locks.
type VerifiedReplacementDetectionInputs struct {
 descriptor string
 snapshot uuid.UUID
 verified VerifiedPerspectiveLinesInputs
}

func(p PerspectiveReplacementDetectionPlan) VerifyInputs(ctx context.Context,snapshot uuid.UUID,sources PerspectiveLinesSourceCatalog,arrays v2optical.PerspectiveSourceArrayCatalog,objects v2input.SessionAnalysisObjectReader)(VerifiedReplacementDetectionInputs,error){
 var zero VerifiedReplacementDetectionInputs
 if _,_,_,_,err:=p.PreparePublication(ctx);err!=nil{return zero,err}
 descriptor,err:=decodeReplacementDetectionDescriptor([]byte(p.descriptor));if err!=nil{return zero,err}
 if snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 who:=auth.Identity{UserID:descriptor.Actor,SessionID:descriptor.Session}
 verified,err:=VerifyPerspectiveLinesInputs(ctx,sources,arrays,objects,p.detector,snapshot,who)
 if err!=nil{return zero,err}
 // Revalidate the complete private packet after all external callbacks/IO.
 if _,_,_,_,err=p.PreparePublication(ctx);err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 return VerifiedReplacementDetectionInputs{descriptor:p.descriptor,snapshot:snapshot,verified:verified},nil
}

// AdmissionJSON is only an IO witness. It cannot grant SQL source authority.
func(v VerifiedReplacementDetectionInputs) AdmissionJSON(ctx context.Context,p PerspectiveReplacementDetectionPlan,snapshot uuid.UUID,bucket string)(json.RawMessage,error){
 if _,_,_,_,err:=p.PreparePublication(ctx);err!=nil{return nil,err}
 if snapshot==uuid.Nil||v.snapshot!=snapshot||v.descriptor!=p.descriptor{return nil,v2input.ErrInvalid}
 descriptor,err:=decodeReplacementDetectionDescriptor([]byte(p.descriptor));if err!=nil{return nil,err}
 if v.verified.snapshot!=snapshot||v.verified.who.UserID!=descriptor.Actor||v.verified.who.SessionID!=descriptor.Session{return nil,v2input.ErrInvalid}
 return v.verified.AdmissionJSON(p.detector,bucket)
}

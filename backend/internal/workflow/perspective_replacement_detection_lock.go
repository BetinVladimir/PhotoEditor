package workflow
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2optical"
)
// Caller must retain tx until the later dedicated persistence/admission phase.
func LockPerspectiveReplacementDetectionPlan(ctx context.Context,tx pgx.Tx,prepared v2optical.PreparedNegativeReplacementInputs,rebuilt,retained []v2optical.VerifiedAdoptedParentArrays,mappings []v2optical.VerifiedNegativeAffectedSources,unchanged []v2optical.VerifiedNegativeUnchangedArrays,raw []byte,snapshot uuid.UUID)(PerspectiveReplacementDetectionPlan,error){
 var zero PerspectiveReplacementDetectionPlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if _,err:=decodeReplacementDetectionDescriptor(raw);err!=nil{return zero,err}
 step,err:=prepared.LockPerspectiveDetectionPreparation(ctx,tx,rebuilt,retained,mappings,unchanged,raw,snapshot);if err!=nil{return zero,err}
 return RestorePerspectiveReplacementDetectionPlan(ctx,step,raw)
}

// Save remains in caller transaction. On any error caller must roll back,
// including failure of post-save fresh descriptor restoration.
func SavePerspectiveReplacementDetectionPlan(ctx context.Context,tx pgx.Tx,prepared v2optical.PreparedNegativeReplacementInputs,rebuilt,retained []v2optical.VerifiedAdoptedParentArrays,mappings []v2optical.VerifiedNegativeAffectedSources,unchanged []v2optical.VerifiedNegativeUnchangedArrays,raw []byte,snapshot uuid.UUID)(PerspectiveReplacementDetectionPlan,error){
 var zero PerspectiveReplacementDetectionPlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if _,err:=decodeReplacementDetectionDescriptor(raw);err!=nil{return zero,err}
 step,err:=prepared.SavePerspectiveDetectionDraft(ctx,tx,rebuilt,retained,mappings,unchanged,raw,snapshot);if err!=nil{return zero,err}
 return RestorePerspectiveReplacementDetectionPlan(ctx,step,raw)
}

func ReadPerspectiveReplacementDetectionPlan(ctx context.Context,tx pgx.Tx,prepared v2optical.PreparedNegativeReplacementInputs,rebuilt,retained []v2optical.VerifiedAdoptedParentArrays,mappings []v2optical.VerifiedNegativeAffectedSources,unchanged []v2optical.VerifiedNegativeUnchangedArrays,expected []byte,snapshot uuid.UUID)(PerspectiveReplacementDetectionPlan,error){
 var zero PerspectiveReplacementDetectionPlan
 if err:=ctx.Err();err!=nil{return zero,err}
 if _,err:=decodeReplacementDetectionDescriptor(expected);err!=nil{return zero,err}
 step,saved,err:=prepared.ReadPerspectiveDetectionDraft(ctx,tx,rebuilt,retained,mappings,unchanged,expected,snapshot);if err!=nil{return zero,err}
 return RestorePerspectiveReplacementDetectionPlan(ctx,step,saved)
}

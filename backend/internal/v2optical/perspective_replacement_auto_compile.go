package v2optical

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// CompileLockedAutomaticPerspectiveReplacement repeats live qualification and
// existing full compiler checks. Caller must separately hold current detector,
// segment artifact and mapped-source locks; this function grants no admission.
func(p PreparedMappedReplacementStep) CompileLockedAutomaticPerspectiveReplacement(ctx context.Context,tx pgx.Tx,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate,lines v2input.Ref,original []byte)(PerspectiveReplacementDraft,error){
 var zero PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil||snapshot==uuid.Nil||len(original)<2||len(original)>1<<20{return zero,v2input.ErrInvalid}
 automatic,err:=p.AutomaticPerspectiveDetection(ctx);if err!=nil{return zero,err};if !automatic{return zero,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 l:=p.parents.step.intent.locator;who:=p.parents.step.intent.who
 if err=LockLocatedPerspectiveQualification(ctx,tx,who,l.Tenant,l.Project,located);err!=nil{return zero,err}
 return p.CompilePerspectiveReplacementDraft(ctx,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,maxPixels,capacity,&lines,append([]byte(nil),original...))
}

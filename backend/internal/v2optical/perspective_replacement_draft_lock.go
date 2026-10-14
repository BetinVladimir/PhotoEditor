package v2optical

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// LockManualPerspectiveReplacementDraft retains the whole ready-wave physical
// authority, original consumer snapshot and live qualification in caller's tx.
// Automatic mode needs a separate persisted fresh-lines authority and cannot
// enter this gate. No persistence, reservation, commit or admission occurs here.
func(p PreparedNegativeReplacementInputs)LockManualPerspectiveReplacementDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,raw []byte,snapshot uuid.UUID,located LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity PerspectiveResourceEstimate)(PerspectiveReplacementDraft,error){
 var zero PerspectiveReplacementDraft
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil||snapshot==uuid.Nil||len(raw)<2||len(raw)>3<<20{return zero,v2input.ErrInvalid}
 // Reject auto mode before any lock acquisition; caller-provided line bytes
 // alone do not establish their adopted producer or original object versions.
 var packet perspectiveReplacementDraftSnapshot
 if json.Unmarshal(raw,&packet)!=nil{return zero,v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams([]byte(packet.Draft.ParamsJCS));if err!=nil{return zero,err}
 if params.Mode!="manual"||params.Lines!=nil||packet.Draft.LinesJCS!=""{return zero,v2input.ErrInvalid}
 qualified,err:=located.Qualification();if err!=nil{return zero,err}
 pin,err:=qualified.Pin();if err!=nil{return zero,err}
 ready,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 var selected *PreparedMappedReplacementStep
 var draft PerspectiveReplacementDraft
 for i:=range ready{
  if ready[i].parents.step.intent.locator.Operation!="optical.perspective"{continue}
  candidate,e:=ready[i].RestorePerspectiveReplacementDraft(ctx,raw,snapshot,pin,locks,qualified.reportRaw,qualified.calibrationRaw,maxPixels,capacity,nil)
  if e!=nil{if ctx.Err()!=nil{return zero,ctx.Err()};continue}
  if selected!=nil{return zero,v2input.ErrInvalid}
  selected=&ready[i];draft=candidate
 }
 if selected==nil{return zero,v2input.ErrInvalid}
 if _,err=p.LockCompleteMappedSources(ctx,tx,rebuilt,retained,mappings,unchanged);err!=nil{return zero,err}
 l:=selected.parents.step.intent.locator;who:=selected.parents.step.intent.who
 var valid bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_replacement_snapshot($1,$2,$3,$4,$5,$6,$7,$8)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,selected.Node().OriginalTask,snapshot).Scan(&valid);err!=nil{return zero,err}
 if !valid{return zero,v2input.ErrExpired}
 if err=LockLocatedPerspectiveQualification(ctx,tx,who,l.Tenant,l.Project,located);err!=nil{return zero,err}
 return draft,ctx.Err()
}

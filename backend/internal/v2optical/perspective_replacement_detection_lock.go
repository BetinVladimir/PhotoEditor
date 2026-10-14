package v2optical
import (
 "context"
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// Retain complete current mapped-wave/array and original snapshot locks. This
// performs no persistence, reservation or activation; caller owns transaction.
func(p PreparedNegativeReplacementInputs) LockPerspectiveDetectionPreparation(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,error){
 var zero PreparedMappedReplacementStep
 if err:=ctx.Err();err!=nil{return zero,err}
 if tx==nil||snapshot==uuid.Nil||len(descriptor)<2||len(descriptor)>3<<20{return zero,v2input.ErrInvalid}
 checked,plan,_,err:=p.checked(ctx);if err!=nil{return zero,err}
 var packet struct{Replacement NegativeReplacementNode `json:"replacement"`;Base v2input.SessionVersionBinding `json:"base"`;Source string `json:"source_jcs"`}
 if json.Unmarshal(descriptor,&packet)!=nil||packet.Replacement.OriginalTask==uuid.Nil{return zero,v2input.ErrInvalid}
 ready,err:=checked.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 var selected *PreparedMappedReplacementStep
 for i:=range ready{
  if ready[i].Node().OriginalTask!=packet.Replacement.OriginalTask{continue}
  if selected!=nil{return zero,v2input.ErrInvalid}
  source,e:=ready[i].PerspectiveDetectionSource(ctx);if e!=nil{return zero,e}
  node,e:=canonical.Marshal(ready[i].Node());if e!=nil{return zero,e}
  expected,e:=canonical.Marshal(packet.Replacement);if e!=nil{return zero,e}
  if !bytes.Equal(node,expected)||source.Base!=packet.Base||string(source.WorkingImage)!=packet.Source{return zero,v2input.ErrInvalid}
  selected=&ready[i]
 }
 if selected==nil{return zero,v2input.ErrInvalid}
 if _,err=checked.LockCompleteMappedSources(ctx,tx,rebuilt,retained,mappings,unchanged);err!=nil{return zero,err}
 rawPlan,err:=canonical.Marshal(plan);if err!=nil{return zero,err}
 l:=selected.parents.step.intent.locator;who:=selected.parents.step.intent.who
 var valid bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_replacement_detector_preparation($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,string(rawPlan),string(descriptor),selected.parents.step.intent.raw,snapshot).Scan(&valid);err!=nil{return zero,err}
 if !valid{return zero,v2input.ErrExpired}
 return *selected,ctx.Err()
}

// Caller must roll back on error and retain tx through its own persistence unit.
func(p PreparedNegativeReplacementInputs) SavePerspectiveDetectionDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,error){
 var zero PreparedMappedReplacementStep
 step,err:=p.LockPerspectiveDetectionPreparation(ctx,tx,rebuilt,retained,mappings,unchanged,descriptor,snapshot);if err!=nil{return zero,err}
 _,plan,_,err:=p.checked(ctx);if err!=nil{return zero,err}
 rawPlan,err:=canonical.Marshal(plan);if err!=nil{return zero,err}
 l:=step.parents.step.intent.locator;who:=step.parents.step.intent.who
 var saved bool
 if err=tx.QueryRow(ctx,"SELECT photo.save_v29_replacement_detector_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,string(rawPlan),string(descriptor),step.parents.step.intent.raw,snapshot).Scan(&saved);err!=nil{return zero,err}
 if !saved{return zero,v2input.ErrExpired}
 return step,ctx.Err()
}

// Read exact existing draft after full current physical-wave checks. Never save
// or repair absent/mismatched state. Caller retains transaction and must rollback
// on error, including post-read restoration failures in workflow.
func(p PreparedNegativeReplacementInputs) ReadPerspectiveDetectionDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,[]byte,error){
 var zero PreparedMappedReplacementStep
 step,err:=p.LockPerspectiveDetectionPreparation(ctx,tx,rebuilt,retained,mappings,unchanged,descriptor,snapshot);if err!=nil{return zero,nil,err}
 _,plan,_,err:=p.checked(ctx);if err!=nil{return zero,nil,err}
 rawPlan,err:=canonical.Marshal(plan);if err!=nil{return zero,nil,err}
 l:=step.parents.step.intent.locator;who:=step.parents.step.intent.who
 var saved *string
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_replacement_detector_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,string(rawPlan),string(descriptor),step.parents.step.intent.raw,snapshot).Scan(&saved);err!=nil{return zero,nil,err}
 if saved==nil||*saved!=string(descriptor){return zero,nil,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return zero,nil,err}
 return step,[]byte(*saved),nil
}

// Proof is request data only; SQL repeats scope and original-intent authority.
// This method first retains complete physical-source locks and exact saved draft.
func(p PreparedNegativeReplacementInputs) PerspectiveDetectionPublicationProof(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,[]byte,error){
 var zero PreparedMappedReplacementStep
 step,_,err:=p.ReadPerspectiveDetectionDraft(ctx,tx,rebuilt,retained,mappings,unchanged,descriptor,snapshot);if err!=nil{return zero,nil,err}
 _,plan,_,err:=p.checked(ctx);if err!=nil{return zero,nil,err}
 l:=step.parents.step.intent.locator;who:=step.parents.step.intent.who
 proof,err:=canonical.Marshal(map[string]any{"project_id":l.Project,"actor_id":who.UserID,"auth_session_id":who.SessionID,"review_id":l.Review,"decision_hash":l.Hash,"plan":plan,"descriptor_jcs":string(descriptor),"original_jcs":step.parents.step.intent.raw,"snapshot_id":snapshot})
 if err!=nil{return zero,nil,err}
 return step,proof,ctx.Err()
}

// EnsurePerspectiveDetectionDraft resumes exact staged/sealed draft state or
// saves a missing draft. Physical source locks precede both paths; stale or
// conflicting existing runs are never replaced. Caller must rollback on error.
func(p PreparedNegativeReplacementInputs) EnsurePerspectiveDetectionDraft(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,error){
 var zero PreparedMappedReplacementStep
 step,err:=p.LockPerspectiveDetectionPreparation(ctx,tx,rebuilt,retained,mappings,unchanged,descriptor,snapshot);if err!=nil{return zero,err}
 _,plan,_,err:=p.checked(ctx);if err!=nil{return zero,err}
 rawPlan,err:=canonical.Marshal(plan);if err!=nil{return zero,err}
 l:=step.parents.step.intent.locator;who:=step.parents.step.intent.who
 var saved bool
 if err=tx.QueryRow(ctx,"SELECT photo.ensure_v29_replacement_detector_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,string(rawPlan),string(descriptor),step.parents.step.intent.raw,snapshot).Scan(&saved);err!=nil{return zero,err}
 if !saved{return zero,v2input.ErrExpired};return step,ctx.Err()
}


// CurrentProof repeats full physical source locks without requiring draft state.
// It does not attest admission or a result; SQL evidence must verify those.
func(p PreparedNegativeReplacementInputs) PerspectiveDetectionCurrentProof(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays,descriptor []byte,snapshot uuid.UUID)(PreparedMappedReplacementStep,[]byte,error){
 var zero PreparedMappedReplacementStep
 step,err:=p.LockPerspectiveDetectionPreparation(ctx,tx,rebuilt,retained,mappings,unchanged,descriptor,snapshot);if err!=nil{return zero,nil,err}
 _,plan,_,err:=p.checked(ctx);if err!=nil{return zero,nil,err}
 l:=step.parents.step.intent.locator;who:=step.parents.step.intent.who
 proof,err:=canonical.Marshal(map[string]any{"project_id":l.Project,"actor_id":who.UserID,"auth_session_id":who.SessionID,"review_id":l.Review,"decision_hash":l.Hash,"plan":plan,"descriptor_jcs":string(descriptor),"original_jcs":step.parents.step.intent.raw,"snapshot_id":snapshot})
 if err!=nil{return zero,nil,err}
 return step,proof,ctx.Err()
}

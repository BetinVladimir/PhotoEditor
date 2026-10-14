package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type automaticPerspectivePublicationSteps struct {
 probe func(context.Context)(string,error)
 save,params,transport,admit func(context.Context)error
 receipt func(context.Context)(v2optical.AutomaticPerspectiveReplacementReceipt,error)
}
func executeAutomaticPerspectivePublication(ctx context.Context,s automaticPerspectivePublicationSteps)(v2optical.AutomaticPerspectiveReplacementReceipt,error){
 var zero v2optical.AutomaticPerspectiveReplacementReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.probe==nil||s.save==nil||s.params==nil||s.transport==nil||s.admit==nil||s.receipt==nil{return zero,v2input.ErrInvalid}
 state,err:=s.probe(ctx);if err!=nil{return zero,err}
 if state!="absent"&&state!="draft"&&state!="admitted"{return zero,v2input.ErrExpired}
 if state!="admitted"{
  for _,phase:=range []func(context.Context)error{s.save,s.params,s.transport,s.admit}{
   if err:=ctx.Err();err!=nil{return zero,err}
   if err:=phase(ctx);err!=nil{
    // Never retry mutations after an ambiguous response. A successful durable
    // read may recover admission; otherwise preserve the original failure.
    if ctx.Err()!=nil{return zero,ctx.Err()}
    receipt,recoveryErr:=s.receipt(ctx)
    if recoveryErr==nil&&ctx.Err()==nil{receipt.RecoveredExisting=true;return receipt,nil}
    return zero,err
   }
  }
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 receipt,err:=s.receipt(ctx);if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 receipt.RecoveredExisting=state=="admitted";return receipt,nil
}

func(r ReplacementDetectionDocuments) ExecuteAutomaticPerspectiveReplacement(ctx context.Context,objects artifactjson.Objects,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.AutomaticPerspectiveReplacementReceipt,error){
 var draft v2optical.PerspectiveReplacementDraft
 return executeAutomaticPerspectivePublication(ctx,automaticPerspectivePublicationSteps{
  probe:func(ctx context.Context)(string,error){
   var state *string
   err:=r.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
    current,err:=r.LockAutomaticPerspectiveDraft(ctx,tx,segments,located,locks,maxPixels,capacity);if err!=nil{return err};draft=current
    proof,err:=(replacementSegmentsRepository{source:r,plan:segments.plan}).lockedProof(ctx,tx);if err!=nil{return err}
    return tx.QueryRow(ctx,"SELECT photo.probe_v29_auto_perspective_publication($1,$2,$3::jsonb,$4)",segments.plan.Scope().Tenant,string(draft.Bytes()),string(proof),r.Bucket).Scan(&state)
   });if err!=nil{return "",err};if state==nil{return "",v2input.ErrExpired};return *state,nil
  },
  save:func(ctx context.Context)error{if objects==nil{return v2input.ErrInvalid};var err error;draft,err=r.SaveAutomaticPerspectiveDraft(ctx,segments,located,locks,maxPixels,capacity);return err},
  params:func(ctx context.Context)error{var err error;draft,err=r.PublishAutomaticPerspectiveParams(ctx,objects,segments,located,locks,maxPixels,capacity);return err},
  transport:func(ctx context.Context)error{_,err:=r.PublishAutomaticPerspectiveTransport(ctx,objects,draft,segments,located,locks,maxPixels,capacity);return err},
  admit:func(ctx context.Context)error{_,err:=r.AdmitAutomaticPerspectiveGraph(ctx,draft,segments,located,locks,maxPixels,capacity);return err},
  receipt:func(ctx context.Context)(v2optical.AutomaticPerspectiveReplacementReceipt,error){return r.ReadAutomaticPerspectiveReceipt(ctx,segments,located,locks,maxPixels,capacity)},
 })
}

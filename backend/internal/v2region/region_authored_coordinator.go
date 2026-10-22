package v2region

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// The canonical facts and crop descriptor are read only through an actual
// admission/adoption implementation. No callback boolean is accepted from HTTP.
type AuthoredRegionChildren interface {
 Inspect(context.Context,AuthoredRegionRecord)(AuthoredRegionChildFacts,error)
 StartRender(context.Context,AuthoredRegionRecord)error
 StartCrop(context.Context,AuthoredRegionRecord)error
 Cancel(context.Context,AuthoredRegionRecord)error
}
type AuthoredRegionChildFacts struct {
 RenderExists,RenderTerminal,RenderCanonical bool
 CropExists,CropTerminal,CropCanonical bool
 AllWritersClosed,AllQuotaSettled bool
}
type AuthoredRegionTicket struct {
 ID uuid.UUID `json:"id"`
 Phase AuthoredRegionPhase `json:"phase"`
 Revision int64 `json:"revision"`
 IntentSHA string `json:"intent_sha256"`
 // Descriptor is obtained by a separate bounded, live-authorized physical read.
}

type AuthoredRegionCoordinator struct { Store AuthoredRegionStore; Children AuthoredRegionChildren }

// Reconcile performs at most one domain action. Durable intent precedes child
// creation. Stable child IDs make interrupted stages recoverable; existing
// child identity must be checked by the child store before reuse. No SQL lock
// spans publication or object IO. Cancellation precedes any new child work.
func(c AuthoredRegionCoordinator)Reconcile(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionTicket,error){
 var empty AuthoredRegionTicket
 if c.Store==nil||c.Children==nil{return empty,ErrRegionUnavailable}
 saved,err:=c.Store.Read(ctx,who,tenant,asset,id);if err!=nil{return empty,err}
 if err=saved.validate();err!=nil{return empty,err}
 if saved.ID!=id||saved.Intent.Tenant!=tenant||saved.Intent.Asset!=asset||saved.Intent.Actor!=who.UserID||saved.Intent.Session!=who.SessionID{return empty,ErrRegionInvalid}
 facts,err:=c.Children.Inspect(ctx,saved);if err!=nil{return empty,err}
 phase,err:=authoredPhase(saved,facts);if err!=nil{return empty,err}
 switch phase {
 case AuthoredRegionCancelling:
  if err=c.Children.Cancel(ctx,saved);err!=nil{return empty,err}
 case AuthoredRegionAdmitted:
  if err=c.Children.StartRender(ctx,saved);err!=nil{return empty,err}
 case AuthoredRegionCropping:
  if facts.RenderCanonical&&!facts.CropExists {
   if err=c.Children.StartCrop(ctx,saved);err!=nil{return empty,err}
  }
 }
 // Authorize again after potentially slow child publication. Stale results
 // cannot be returned as ready based on an earlier access/revision snapshot.
 after,err:=c.Store.Read(ctx,who,tenant,asset,id);if err!=nil{return empty,err}
 if err=after.validate();err!=nil{return empty,err}
 if after.ID!=saved.ID||after.Revision<saved.Revision{return empty,ErrRegionInvalid}
 if after.SHA!=saved.SHA||after.RenderBatch!=saved.RenderBatch||after.CropRun!=saved.CropRun{return empty,v2input.ErrConflict}
 latest,err:=c.Children.Inspect(ctx,after);if err!=nil{return empty,err}
 // Inspect may perform independent canonical/receipt reads. Authorize once
 // more after it, so a completed crop cannot hide a concurrent draft change or
 // cancellation observed during those reads. This is the response's final
 // authority point; it does not grant an object URL beyond descriptor checks.
 final,err:=c.Store.Read(ctx,who,tenant,asset,id);if err!=nil{return empty,err}
 if final.Revision<after.Revision||final.ID!=saved.ID{return empty,ErrRegionInvalid}
 if final.SHA!=saved.SHA||final.RenderBatch!=saved.RenderBatch||final.CropRun!=saved.CropRun{return empty,v2input.ErrConflict}
 if err=final.validate();err!=nil{return empty,err}
 phase,err=authoredPhase(final,latest);if err!=nil{return empty,err}
 return AuthoredRegionTicket{id,phase,final.Revision,final.SHA},nil
}
func authoredPhase(r AuthoredRegionRecord,f AuthoredRegionChildFacts)(AuthoredRegionPhase,error){
 return nextAuthoredRegionPhase(authoredRegionFacts{Current:r.Current,CancelRequested:r.CancelRequested,
 RenderExists:f.RenderExists,RenderTerminal:f.RenderTerminal,RenderCanonical:f.RenderCanonical,
 CropExists:f.CropExists,CropTerminal:f.CropTerminal,CropCanonical:f.CropCanonical,
 AllWritersClosed:f.AllWritersClosed,AllQuotaSettled:f.AllQuotaSettled})
}
func(c AuthoredRegionCoordinator)Cancel(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionTicket,error){
 if c.Store==nil{return AuthoredRegionTicket{},ErrRegionUnavailable}
 if err:=c.Store.Cancel(ctx,who,tenant,asset,id);err!=nil{return AuthoredRegionTicket{},err}
 return c.Reconcile(ctx,who,tenant,asset,id)
}

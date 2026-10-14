package delivery
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
// StageWatermarkSelectionSources starts from already persisted selection bytes.
// The parent shell commits before any object IO. Source preparation rechecks
// the saved pending request/config and freezes one version before reading bytes.
// On failure partial durable pins remain available for exact retry.
func(s Service)StageWatermarkSelectionSources(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,pendingRaw []byte,bucket string,objects v2input.SessionAnalysisObjectReader)([]PreparedWatermarkPin,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return nil,err}
 if s.DB==nil||objects==nil||len(bucket)<3||len(bucket)>63{return nil,domain.NewError("DELIVERY_UNAVAILABLE",503)}
 pendingRaw,err=intent.ownWatermarkSelectionPending(ctx,who.SessionID,pendingRaw);if err!=nil{return nil,err}
 pending,err:=intent.DecodeWatermarkSelectionPending(ctx,who.SessionID,pendingRaw);if err!=nil{return nil,err}
 if err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{return intent.BeginWatermarkSelectionRun(ctx,tx,who,tid,pending)});err!=nil{return nil,err}
 pins,err:=PrepareWatermarkInventory(ctx,s,who,tid,intent,pending.Catalog.Presets,bucket,objects);if err!=nil{return nil,err}
 // The same immutable payload must now compile under the verified pin path.
 if _,err=intent.DecodeWatermarkPending(ctx,who.SessionID,pendingRaw,pins);err!=nil{return nil,err}
 return pins,ctx.Err()
}

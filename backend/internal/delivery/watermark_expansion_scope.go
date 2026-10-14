package delivery
import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
const watermarkExpansionScopeQuery=`SELECT project_id,actor_id FROM photo.read_v30_watermark_expansion_scope($1,$2,$3,$4,$5)`
// ReadWatermarkScope requires a tenant-scoped scheduler transaction/adapter.
// It derives actor/project from current persisted authority, never a task payload.
func(s ExpansionLeaseStore)ReadWatermarkScope(ctx context.Context,lease ExpansionLease)(WatermarkPinScope,error){
 var zero WatermarkPinScope
 if err:=ctx.Err();err!=nil{return zero,err};if s.DB==nil||!lease.valid(){return zero,v2input.ErrInvalid}
 var project,actor uuid.UUID
 err:=s.DB.QueryRow(ctx,watermarkExpansionScopeQuery,lease.Tenant,lease.Run,lease.Expansion,lease.Token,lease.ParentTask).Scan(&project,&actor)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 if project==uuid.Nil||actor==uuid.Nil{return zero,v2input.ErrInvalid};if err=ctx.Err();err!=nil{return zero,err}
 return WatermarkPinScope{Tenant:lease.Tenant,Project:project,Actor:actor,Parent:lease.Run},nil
}
